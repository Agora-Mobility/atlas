package verification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/filetype"
	"github.com/Agora-Mobility/atlas/shared/imagemeta"
	"github.com/Agora-Mobility/atlas/usecases/verification/dtos"
)

func objectKey(providerID, verificationID uuid.UUID, documentType enums.DocumentType, slot enums.DocumentSlot, fileID uuid.UUID, extension string) string {
	return fmt.Sprintf("documents/%s/%s/%s/%s/%s%s", providerID, verificationID, documentType, slot, fileID, extension)
}

func toUploadView(document *entities.ProviderDocument, file *entities.DocumentFile) *dtos.UploadView {
	checks := file.QualityChecks
	return &dtos.UploadView{
		ID: file.ID, DocumentType: document.Type.String(), Subject: document.Subject.String(), Slot: file.Slot.String(), Version: file.Version, Status: string(file.Status),
		ContentType: file.ContentType, SizeBytes: file.SizeBytes, Width: file.Width, Height: file.Height, Reasons: file.RejectionCodes, CreatedAt: file.CreatedAt,
		Quality: dtos.QualityView{
			TextLegible: checks.TextLegible, NoShadowOnData: checks.NoShadowOnData, CornersVisible: checks.CornersVisible, NoGlare: checks.NoGlare,
			Sharp: checks.Sharp, WellLit: checks.WellLit, MetadataRemoved: checks.MetadataRemoved,
		},
	}
}

func (u *Usecase) uploadSpec(s *session, in dtos.StartUploadInput) (entities.DocumentSpec, enums.DocumentSlot, enums.DocumentSubject, error) {
	documentType := enums.DocumentType(in.DocumentType)
	spec, known := entities.SpecOf(documentType)
	if !known || !spec.HasFiles() {
		return spec, "", "", ErrUnknownDocument
	}
	subject := enums.DocumentSubject(in.Subject)
	if subject == "" {
		subject = enums.DocumentSubjectProvider
		if person := entities.PersonSubject(s.verification.Kind); entities.AcceptsDocument(s.verification.Kind, documentType, person) {
			subject = person
		}
	}
	if !entities.AcceptsDocument(s.verification.Kind, documentType, subject) {
		return spec, "", "", ErrDocumentNotInPlan
	}
	slot := enums.DocumentSlot(in.Slot)
	if !spec.AcceptsSlot(slot) {
		return spec, "", "", entities.NewRuleError("INVALID_SLOT", "slot", "Este documento não tem esta parte.")
	}
	if !spec.Allows(slot, in.ContentType) {
		return spec, "", "", entities.NewRuleError(CodeInvalidFileType, "content_type", "Este tipo de ficheiro não é aceite aqui.").With("allowed", spec.ContentTypes(slot))
	}
	if in.Size > u.Settings.MaxFileBytes {
		return spec, "", "", entities.NewRuleError(CodeFileTooLarge, "size", "O ficheiro é demasiado grande.").With("max_bytes", u.Settings.MaxFileBytes)
	}
	return spec, slot, subject, nil
}

func (u *Usecase) StartUpload(ctx context.Context, providerID uuid.UUID, in dtos.StartUploadInput) (*dtos.StartedUpload, error) {
	s, err := u.editable(ctx, providerID)
	if err != nil {
		return nil, err
	}
	spec, slot, subject, err := u.uploadSpec(s, in)
	if err != nil {
		return nil, err
	}
	document, err := u.documentFor(s, spec.Type, subject)
	if err != nil {
		return nil, err
	}
	if slot == enums.DocumentSlotExtra && len(document.Accepted(enums.DocumentSlotExtra)) >= spec.MaxExtras {
		return nil, ErrTooManyExtras
	}
	fileID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	extension, _ := filetype.Extension(in.ContentType)
	now := u.now()
	file := &entities.DocumentFile{
		ID: fileID, DocumentID: document.ID, ProviderID: providerID, Slot: slot, Status: entities.FilePendingUpload, ContentType: in.ContentType, SizeBytes: in.Size,
		ObjectKey: objectKey(providerID, s.verification.ID, spec.Type, slot, fileID, extension), RejectionCodes: []string{}, CreatedAt: now, UpdatedAt: now,
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Documents.SaveDocument(ctx, document); err != nil {
			return err
		}
		if file.Version, err = u.Documents.NextFileVersion(ctx, document.ID, slot); err != nil {
			return err
		}
		return u.Documents.SaveFile(ctx, file)
	})
	if err != nil {
		return nil, err
	}
	signed, err := u.Storage.PresignUpload(ctx, file.ObjectKey, in.ContentType, in.Size)
	if err != nil {
		return nil, err
	}
	return &dtos.StartedUpload{UploadID: file.ID, UploadURL: signed.URL, Headers: signed.Headers, ExpiresAt: signed.ExpiresAt, Version: file.Version}, nil
}

func (u *Usecase) ownFile(ctx context.Context, providerID, uploadID uuid.UUID) (*entities.ProviderDocument, *entities.DocumentFile, error) {
	file, err := u.Documents.File(ctx, providerID, uploadID)
	if err != nil {
		return nil, nil, err
	}
	if file == nil {
		return nil, nil, ErrUploadNotFound
	}
	document, err := u.Documents.DocumentByID(ctx, file.DocumentID)
	if err != nil {
		return nil, nil, err
	}
	if document == nil || document.ProviderID != providerID {
		return nil, nil, ErrUploadNotFound
	}
	return document, file, nil
}

func (u *Usecase) ConfirmUpload(ctx context.Context, providerID, uploadID uuid.UUID) (*dtos.UploadView, error) {
	document, file, err := u.ownFile(ctx, providerID, uploadID)
	if err != nil {
		return nil, err
	}
	if file.Status != entities.FilePendingUpload {
		return toUploadView(document, file), nil
	}
	if _, err := u.editable(ctx, providerID); err != nil {
		return nil, err
	}
	object, err := u.Storage.Stat(ctx, file.ObjectKey)
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, ErrFileNotUploaded
	}
	if object.SizeBytes > u.Settings.MaxFileBytes {
		u.discard(ctx, file.ObjectKey)
		return nil, entities.NewRuleError(CodeFileTooLarge, "size", "O ficheiro é demasiado grande.").With("max_bytes", u.Settings.MaxFileBytes)
	}
	head, err := u.Storage.FirstBytes(ctx, file.ObjectKey, filetype.SniffBytes)
	if err != nil {
		return nil, err
	}
	spec, _ := entities.SpecOf(document.Type)
	if detected := filetype.Detect(head); detected != file.ContentType || !spec.Allows(file.Slot, detected) {
		u.discard(ctx, file.ObjectKey)
		return nil, entities.NewRuleError(CodeInvalidFileType, "file", "O conteúdo do ficheiro não corresponde ao tipo indicado.").With("detected", detected)
	}
	now := u.now()
	file.Status, file.SizeBytes, file.UpdatedAt = entities.FileProcessing, object.SizeBytes, now
	document.Edited(now)
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Documents.SaveDocument(ctx, document); err != nil {
			return err
		}
		if err := u.Documents.SaveFile(ctx, file); err != nil {
			return err
		}
		if err := u.publish(ctx, events.ProviderDocumentUploadedV1{ProviderID: providerID, DocumentID: document.ID, FileID: file.ID, OccurredAt: now}); err != nil {
			return err
		}
		return u.audit(ctx, providerActor(providerID), "provider_document.upload", auditEntityDocument, document.ID.String(), map[string]any{
			"type": document.Type, "subject": document.Subject, "slot": file.Slot, "version": file.Version, "file_id": file.ID, "content_type": file.ContentType, "size_bytes": file.SizeBytes,
		})
	})
	if err != nil {
		return nil, err
	}
	return toUploadView(document, file), nil
}

func (u *Usecase) Upload(ctx context.Context, providerID, uploadID uuid.UUID) (*dtos.UploadView, error) {
	document, file, err := u.ownFile(ctx, providerID, uploadID)
	if err != nil {
		return nil, err
	}
	return toUploadView(document, file), nil
}

func (u *Usecase) UploadURL(ctx context.Context, providerID, uploadID uuid.UUID) (*dtos.ReadURL, error) {
	_, file, err := u.ownFile(ctx, providerID, uploadID)
	if err != nil {
		return nil, err
	}
	if file.Status == entities.FilePendingUpload {
		return nil, ErrFileNotUploaded
	}
	signed, err := u.Storage.PresignDownloadFor(ctx, file.ObjectKey, u.Settings.ReadURLTTL)
	if err != nil {
		return nil, err
	}
	return &dtos.ReadURL{URL: signed.URL, ExpiresAt: signed.ExpiresAt}, nil
}

func (u *Usecase) discard(ctx context.Context, key string) {
	if err := u.Storage.Delete(ctx, key); err != nil {
		logger.FromContext(ctx).Warn("stored object not removed", zap.Error(err))
	}
}

func (u *Usecase) OnDocumentUploaded(ctx context.Context, env events.Envelope) error {
	var event events.ProviderDocumentUploadedV1
	if err := env.DecodePayload(&event); err != nil {
		return err
	}
	return u.ProcessFile(ctx, event.FileID)
}

func (u *Usecase) ProcessFile(ctx context.Context, fileID uuid.UUID) error {
	file, err := u.Documents.FileByID(ctx, fileID)
	if err != nil || file == nil || file.Status != entities.FileProcessing {
		return err
	}
	document, err := u.Documents.DocumentByID(ctx, file.DocumentID)
	if err != nil || document == nil {
		return err
	}
	content, err := u.Storage.Read(ctx, file.ObjectKey, u.Settings.MaxFileBytes)
	if err != nil {
		return err
	}
	if file.IsImage() {
		if content, err = u.cleanImage(ctx, file, content); err != nil {
			return err
		}
	}
	if file.IsImage() && len(file.RejectionCodes) == 0 {
		report, err := u.Quality.Check(ctx, services.ImageToCheck{DocumentType: document.Type, Slot: file.Slot, ContentType: file.ContentType, Content: content})
		if err != nil {
			return err
		}
		checks := &file.QualityChecks
		checks.TextLegible, checks.NoShadowOnData, checks.CornersVisible = report.TextLegible, report.NoShadowOnData, report.CornersVisible
		checks.NoGlare, checks.Sharp, checks.WellLit = report.NoGlare, report.Sharp, report.WellLit
		file.RejectionCodes = slices.Clone(report.Reasons)
	}
	digest := sha256.Sum256(content)
	file.SHA256, file.SizeBytes, file.UpdatedAt = hex.EncodeToString(digest[:]), int64(len(content)), u.now()
	file.Status = entities.FileAccepted
	if len(file.RejectionCodes) > 0 {
		file.Status = entities.FileQualityFailed
	}
	return u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if file.Status == entities.FileAccepted && file.Slot != enums.DocumentSlotExtra {
			if err := u.Documents.SupersedeFiles(ctx, file.DocumentID, file.Slot, file.ID, file.UpdatedAt); err != nil {
				return err
			}
		}
		return u.Documents.SaveFile(ctx, file)
	})
}

func (u *Usecase) cleanImage(ctx context.Context, file *entities.DocumentFile, content []byte) ([]byte, error) {
	if width, height, ok := imagemeta.Dimensions(file.ContentType, content); ok {
		file.Width, file.Height = &width, &height
	}
	stripped, removed, err := imagemeta.Strip(file.ContentType, content)
	if errors.Is(err, imagemeta.ErrMalformed) {
		file.RejectionCodes = []string{services.QualityTextNotLegible}
		return content, nil
	}
	if err != nil {
		return nil, err
	}
	if removed {
		if err := u.Storage.Write(ctx, file.ObjectKey, file.ContentType, stripped); err != nil {
			return nil, err
		}
	}
	file.QualityChecks.MetadataRemoved = removed || file.ContentType != entities.ContentTypeHEIC
	return stripped, nil
}
