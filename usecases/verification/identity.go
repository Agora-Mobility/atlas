package verification

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/usecases/verification/dtos"
)

const (
	failureUnavailable = "unavailable"
	failureUnreadable  = "unreadable"
	maxNameLength      = 160
)

var identityFields = []string{"full_name", "number", "birth_date", "issued_at", "expires_at"}

func (u *Usecase) identitySubject(s *session, raw string) (enums.DocumentSubject, error) {
	subject := subjectOr(raw, entities.PersonSubject(s.verification.Kind))
	if !entities.AcceptsDocument(s.verification.Kind, enums.DocumentTypeIDCard, subject) {
		return "", ErrDocumentNotInPlan
	}
	return subject, nil
}

func (u *Usecase) manualAllowed(extraction *entities.Extraction) bool {
	return extraction != nil && extraction.Status == entities.ExtractionFailed && (extraction.Failure == failureUnavailable || extraction.Attempts >= u.Settings.MaxAttempts)
}

func (u *Usecase) extractionView(document *entities.ProviderDocument) *dtos.ExtractionView {
	view := &dtos.ExtractionView{Status: "not_started", MaxAttempts: u.Settings.MaxAttempts}
	if document == nil || document.Details.Extraction == nil {
		return view
	}
	extraction := document.Details.Extraction
	view.Status, view.Attempts, view.Failure = extraction.Status, extraction.Attempts, extraction.Failure
	view.ManualEntryAllowed = u.manualAllowed(extraction)
	if extraction.Status == entities.ExtractionCompleted {
		view.Fields, view.Confidence = extraction.Fields, extraction.Confidence
		if expires, err := time.Parse(time.DateOnly, extraction.Fields["expires_at"]); err == nil && !entities.IsFutureDate(expires, u.now()) {
			view.RequiresRenewalSlip = !document.HasAccepted(enums.DocumentSlotRenewalReceipt)
		}
	}
	return view
}

func missingSlots(document *entities.ProviderDocument, slots ...enums.DocumentSlot) []string {
	missing := []string{}
	for _, slot := range slots {
		if document == nil || !document.HasAccepted(slot) {
			missing = append(missing, slot.String())
		}
	}
	return missing
}

func (u *Usecase) StartExtraction(ctx context.Context, providerID uuid.UUID, in dtos.SubjectInput) (*dtos.ExtractionView, error) {
	s, err := u.editable(ctx, providerID)
	if err != nil {
		return nil, err
	}
	subject, err := u.identitySubject(s, in.Subject)
	if err != nil {
		return nil, err
	}
	document := s.document(enums.DocumentTypeIDCard, subject)
	if missing := missingSlots(document, enums.DocumentSlotFront, enums.DocumentSlotBack); len(missing) > 0 {
		return nil, entities.NewRuleError(CodeImagesRequired, "files", "Envia a frente e o verso do BI antes da leitura.").With("missing_slots", missing)
	}
	extraction := document.Details.Extraction
	if extraction != nil && extraction.Status == entities.ExtractionProcessing {
		return u.extractionView(document), nil
	}
	if extraction != nil && (extraction.Attempts >= u.Settings.MaxAttempts || extraction.Failure == failureUnavailable) {
		return nil, entities.NewRuleError(CodeExtractionMax, "", "Não conseguimos ler o BI. Escreve os dados à mão.").With("attempts", extraction.Attempts).With("max_attempts", u.Settings.MaxAttempts)
	}
	attempts := 1
	if extraction != nil {
		attempts = extraction.Attempts + 1
	}
	now := u.now()
	document.Details.Extraction = &entities.Extraction{Status: entities.ExtractionProcessing, Attempts: attempts, RequestedAt: &now}
	document.UpdatedAt = now
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Documents.SaveDocument(ctx, document); err != nil {
			return err
		}
		return u.publish(ctx, events.ProviderIdentityReadRequestedV1{ProviderID: providerID, DocumentID: document.ID, Attempt: attempts, OccurredAt: now})
	})
	if err != nil {
		return nil, err
	}
	return u.extractionView(document), nil
}

func (u *Usecase) Extraction(ctx context.Context, providerID uuid.UUID, rawSubject string) (*dtos.ExtractionView, error) {
	s, err := u.open(ctx, providerID)
	if err != nil {
		return nil, err
	}
	subject, err := u.identitySubject(s, rawSubject)
	if err != nil {
		return nil, err
	}
	return u.extractionView(s.document(enums.DocumentTypeIDCard, subject)), nil
}

func (u *Usecase) OnIdentityReadRequested(ctx context.Context, env events.Envelope) error {
	var event events.ProviderIdentityReadRequestedV1
	if err := env.DecodePayload(&event); err != nil {
		return err
	}
	return u.ReadIdentity(ctx, event.DocumentID)
}

func (u *Usecase) ReadIdentity(ctx context.Context, documentID uuid.UUID) error {
	document, err := u.Documents.DocumentByID(ctx, documentID)
	if err != nil || document == nil || document.Details.Extraction == nil || document.Details.Extraction.Status != entities.ExtractionProcessing {
		return err
	}
	front, back := document.Accepted(enums.DocumentSlotFront), document.Accepted(enums.DocumentSlotBack)
	if len(front) == 0 || len(back) == 0 {
		return u.finishExtraction(ctx, document, nil, failureUnreadable)
	}
	images := services.IdentityImages{FrontContentType: front[0].ContentType, BackContentType: back[0].ContentType}
	if images.Front, err = u.Storage.Read(ctx, front[0].URL, u.Settings.MaxFileBytes); err != nil {
		return err
	}
	if images.Back, err = u.Storage.Read(ctx, back[0].URL, u.Settings.MaxFileBytes); err != nil {
		return err
	}
	read, err := u.OCR.ReadIdentityCard(ctx, images)
	switch {
	case errors.Is(err, services.ErrExtractionUnavailable):
		return u.finishExtraction(ctx, document, nil, failureUnavailable)
	case errors.Is(err, services.ErrExtractionFailed):
		return u.finishExtraction(ctx, document, nil, failureUnreadable)
	case err != nil:
		return err
	}
	return u.finishExtraction(ctx, document, read, "")
}

func (u *Usecase) finishExtraction(ctx context.Context, document *entities.ProviderDocument, read *services.ExtractedIdentity, failure string) error {
	now := u.now()
	extraction := document.Details.Extraction
	extraction.FinishedAt, extraction.Failure = &now, failure
	if read == nil {
		extraction.Status = entities.ExtractionFailed
	} else {
		extraction.Status, extraction.Fields, extraction.Confidence = entities.ExtractionCompleted, read.Fields, read.Confidence
	}
	document.UpdatedAt = now
	return u.Documents.SaveDocument(ctx, document)
}

type identityData struct {
	fullName  string
	number    string
	birthDate time.Time
	issuedAt  time.Time
	expiresAt time.Time
}

func (u *Usecase) parseIdentity(in dtos.IdentityInput, subject enums.DocumentSubject) (*identityData, error) {
	fullName, err := cleanText("full_name", in.FullName, maxNameLength)
	if err != nil {
		return nil, err
	}
	data := &identityData{fullName: fullName, number: entities.NormalizeIDNumber(in.Number)}
	if !entities.ValidIDNumber(data.number) {
		return nil, customerrors.FieldError("number", "O número do BI tem 9 algarismos, 2 letras e 3 algarismos, como 005487123LA041.")
	}
	if data.birthDate, err = parseDate("birth_date", in.BirthDate); err != nil {
		return nil, err
	}
	if data.issuedAt, err = parseDate("issued_at", in.IssuedAt); err != nil {
		return nil, err
	}
	if data.expiresAt, err = parseDate("expires_at", in.ExpiresAt); err != nil {
		return nil, err
	}
	now := u.now()
	if entities.IsFutureDate(data.issuedAt, now) {
		return nil, customerrors.FieldError("issued_at", "A data de emissão não pode ser futura.")
	}
	if !data.expiresAt.After(data.issuedAt) {
		return nil, customerrors.FieldError("expires_at", "A data de expiração tem de ser posterior à data de emissão.")
	}
	if subject != enums.DocumentSubjectVehicleOwner && entities.AgeAt(data.birthDate, now) < u.Settings.MinProviderAge {
		return nil, entities.NewRuleError(CodeUnderAge, "birth_date", "É preciso ter a idade mínima para prestar serviços.").With("min_age", u.Settings.MinProviderAge)
	}
	return data, nil
}

func editedFields(extraction *entities.Extraction, data *identityData) []string {
	if extraction == nil || extraction.Status != entities.ExtractionCompleted {
		return []string{}
	}
	values := map[string]string{
		"number": data.number, "birth_date": data.birthDate.Format(time.DateOnly), "issued_at": data.issuedAt.Format(time.DateOnly), "expires_at": data.expiresAt.Format(time.DateOnly),
	}
	edited := []string{}
	for _, field := range identityFields {
		read := extraction.Fields[field]
		switch field {
		case "full_name":
			if !entities.SameName(read, data.fullName) {
				edited = append(edited, field)
			}
		case "number":
			if entities.NormalizeIDNumber(read) != values[field] {
				edited = append(edited, field)
			}
		default:
			if read != values[field] {
				edited = append(edited, field)
			}
		}
	}
	return edited
}

func (u *Usecase) ConfirmIdentity(ctx context.Context, providerID uuid.UUID, in dtos.IdentityInput) (*StepResult, error) {
	s, err := u.editable(ctx, providerID)
	if err != nil {
		return nil, err
	}
	subject, err := u.identitySubject(s, in.Subject)
	if err != nil {
		return nil, err
	}
	document := s.document(enums.DocumentTypeIDCard, subject)
	if missing := missingSlots(document, enums.DocumentSlotFront, enums.DocumentSlotBack); len(missing) > 0 {
		return nil, entities.NewRuleError(CodeImagesRequired, "files", "Envia a frente e o verso do BI.").With("missing_slots", missing)
	}
	extraction := document.Details.Extraction
	manual := in.Source == entities.SourceManual
	if manual && subject != enums.DocumentSubjectVehicleOwner && !u.manualAllowed(extraction) {
		return nil, entities.NewRuleError(CodeManualEntryForbidden, "source", "Tenta primeiro a leitura automática do BI.")
	}
	if !manual && (extraction == nil || extraction.Status != entities.ExtractionCompleted) {
		return nil, entities.NewRuleError(CodeExtractionNotDone, "source", "A leitura do BI ainda não terminou.")
	}
	data, err := u.parseIdentity(in, subject)
	if err != nil {
		return nil, err
	}
	now := u.now()
	document.Unflag(entities.FlagExpiredWithReceipt)
	if !entities.IsFutureDate(data.expiresAt, now) {
		if !document.HasAccepted(enums.DocumentSlotRenewalReceipt) {
			return nil, entities.NewRuleError(CodeIdentityExpired, "expires_at", "O BI está caducado. Envia o recibo de renovação do SIAC.").With("expires_at", data.expiresAt.Format(time.DateOnly))
		}
		document.Flag(entities.FlagExpiredWithReceipt)
	}
	edited := editedFields(extraction, data)
	if manual {
		edited = append([]string{}, identityFields...)
	}
	document.Unflag(entities.FlagManualEntry)
	document.Unflag(entities.FlagEditedAfterOCR)
	switch {
	case manual:
		document.Flag(entities.FlagManualEntry)
	case len(edited) > 0:
		document.Flag(entities.FlagEditedAfterOCR)
	}
	document.Number, document.IssuedAt, document.ExpiresAt = data.number, &data.issuedAt, &data.expiresAt
	document.Details.FullName, document.Details.BirthDate, document.Details.Source = data.fullName, data.birthDate.Format(time.DateOnly), in.Source
	document.Details.ManuallyEditedFields = edited
	document.Edited(now)
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Documents.SaveDocument(ctx, document); err != nil {
			return err
		}
		if subject == entities.PersonSubject(s.verification.Kind) && s.verification.Kind == enums.VerificationKindDriver {
			s.provider.IDNumber, s.provider.BirthDate = data.number, &data.birthDate
			if err := u.Providers.Save(ctx, s.provider); err != nil {
				return err
			}
		}
		return u.audit(ctx, providerActor(providerID), "provider_document.identity_confirmed", auditEntityDocument, document.ID.String(), map[string]any{
			"subject": subject, "source": in.Source, "manually_edited_fields": edited, "review_flags": document.ReviewFlags,
		})
	})
	if err != nil {
		return nil, err
	}
	item := enums.VerificationItemIdentityCard
	switch subject {
	case enums.DocumentSubjectLegalRepresentative:
		item = enums.VerificationItemLegalRepresentative
	case enums.DocumentSubjectVehicleOwner:
		item = enums.VerificationItemVehicleRegistration
	}
	return u.stepView(s, item), nil
}

func (u *Usecase) selfieSubject(s *session, raw string) (enums.DocumentSubject, error) {
	subject := subjectOr(raw, entities.PersonSubject(s.verification.Kind))
	if !entities.AcceptsDocument(s.verification.Kind, enums.DocumentTypeSelfie, subject) {
		return "", ErrDocumentNotInPlan
	}
	return subject, nil
}

func (u *Usecase) StartLiveness(ctx context.Context, providerID uuid.UUID, in dtos.SubjectInput) (*dtos.LivenessSessionView, error) {
	s, err := u.editable(ctx, providerID)
	if err != nil {
		return nil, err
	}
	subject, err := u.selfieSubject(s, in.Subject)
	if err != nil {
		return nil, err
	}
	document, err := u.documentFor(s, enums.DocumentTypeSelfie, subject)
	if err != nil {
		return nil, err
	}
	liveness := document.Details.Liveness
	if liveness == nil {
		liveness = &entities.Liveness{}
	}
	started, err := u.Liveness.StartSession(ctx)
	if err != nil {
		return nil, err
	}
	liveness.SessionID, liveness.Challenge = started.ID, started.Challenge
	if liveness.Status != entities.LivenessNeedsReview && liveness.Status != entities.LivenessAccepted {
		liveness.Status = entities.LivenessPending
	}
	document.Details.Liveness, document.UpdatedAt = liveness, u.now()
	if err := u.Documents.SaveDocument(ctx, document); err != nil {
		return nil, err
	}
	return &dtos.LivenessSessionView{SessionID: started.ID, Challenge: started.Challenge, ExpiresAt: started.ExpiresAt, Attempts: liveness.Attempts, MaxAttempts: u.Settings.MaxAttempts}, nil
}

func (u *Usecase) SubmitSelfie(ctx context.Context, providerID uuid.UUID, in dtos.SelfieInput) (*dtos.SelfieView, error) {
	s, err := u.editable(ctx, providerID)
	if err != nil {
		return nil, err
	}
	subject, err := u.selfieSubject(s, in.Subject)
	if err != nil {
		return nil, err
	}
	document := s.document(enums.DocumentTypeSelfie, subject)
	if document == nil || document.Details.Liveness == nil || document.Details.Liveness.SessionID != in.SessionID {
		return nil, ErrLivenessSession
	}
	file, err := u.Documents.File(ctx, providerID, in.UploadID)
	if err != nil {
		return nil, err
	}
	if file == nil || file.DocumentID != document.ID || file.Slot != enums.DocumentSlotMain {
		return nil, ErrUploadNotFound
	}
	if file.Status == entities.FilePendingUpload {
		return nil, ErrUploadNotReady
	}
	image, err := u.Storage.Read(ctx, file.URL, u.Settings.MaxFileBytes)
	if err != nil {
		return nil, err
	}
	result, err := u.Liveness.Verify(ctx, services.LivenessCheck{SessionID: in.SessionID, ContentType: file.ContentType, Image: image})
	if err != nil {
		return nil, err
	}
	now := u.now()
	liveness := document.Details.Liveness
	liveness.Attempts++
	liveness.CheckedAt, liveness.FailureReason = &now, result.Reason
	switch {
	case result.Outcome == services.LivenessPassed:
		liveness.Status = entities.LivenessAccepted
		document.Unflag(entities.FlagLivenessNotVerified)
	case result.Outcome == services.LivenessUnverified || liveness.Attempts >= u.Settings.MaxAttempts:
		liveness.Status = entities.LivenessNeedsReview
		document.Flag(entities.FlagLivenessNotVerified)
	default:
		liveness.Status = entities.LivenessFailed
	}
	document.Edited(now)
	if err := u.saveDocument(ctx, document, "provider_document.liveness", map[string]any{"subject": subject, "status": liveness.Status, "attempts": liveness.Attempts, "reason": result.Reason}); err != nil {
		return nil, err
	}
	view := &dtos.SelfieView{Status: liveness.Status, Attempts: liveness.Attempts, MaxAttempts: u.Settings.MaxAttempts, Reason: result.Reason}
	if liveness.Status == entities.LivenessFailed {
		return nil, entities.NewRuleError(CodeLivenessFailed, "", "Não conseguimos confirmar que és tu. Tenta outra vez.").
			With("attempts", liveness.Attempts).With("max_attempts", u.Settings.MaxAttempts).With("reason", result.Reason)
	}
	return view, nil
}
