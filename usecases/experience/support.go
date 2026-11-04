package experience

import (
	"context"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/experience/dtos"
)

func toTicketView(t *entities.SupportTicket) dtos.TicketView {
	return dtos.TicketView{ID: t.ID, Code: t.Code, RequestID: t.RequestID, TopicID: t.TopicID, Subject: t.Subject, Status: t.Status, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
}

func (u *Usecase) OpenTicket(ctx context.Context, userID uuid.UUID, requestID *uuid.UUID, in dtos.OpenTicketInput) (*dtos.TicketDetail, error) {
	subject, body, err := entities.NormalizeTicketText(in.Subject, in.Message)
	if err != nil {
		return nil, err
	}
	if requestID != nil {
		request, err := u.Requests.Find(ctx, *requestID)
		if err != nil {
			return nil, err
		}
		if request == nil || (!request.IsCustomer(userID) && !request.IsProvider(userID)) {
			return nil, ErrRequestNotFound
		}
	}
	if in.TopicID != nil {
		topic, err := u.Reference.Item(ctx, *in.TopicID)
		if err != nil {
			return nil, err
		}
		if topic == nil || topic.Kind != entities.CatalogHelpTopic || !topic.IsActive {
			return nil, entities.ErrCatalogItemNotFound
		}
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	now := u.now()
	ticket := &entities.SupportTicket{ID: id, UserID: userID, RequestID: requestID, TopicID: in.TopicID, Subject: subject, Status: entities.TicketOpen, CreatedAt: now, UpdatedAt: now}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		if ticket.Code, err = u.Support.NextCode(ctx); err != nil {
			return err
		}
		if err := u.Support.Create(ctx, ticket); err != nil {
			return err
		}
		return u.Support.AddMessage(ctx, &entities.SupportMessage{TicketID: id, SenderType: entities.TicketSenderUser, SenderID: userID, Body: body, CreatedAt: now})
	})
	if err != nil {
		return nil, err
	}
	return u.ticketDetail(ctx, ticket)
}

func (u *Usecase) ticketDetail(ctx context.Context, ticket *entities.SupportTicket) (*dtos.TicketDetail, error) {
	messages, err := u.Support.Messages(ctx, ticket.ID)
	if err != nil {
		return nil, err
	}
	detail := &dtos.TicketDetail{TicketView: toTicketView(ticket), Messages: make([]dtos.TicketMessage, 0, len(messages))}
	for i := range messages {
		detail.Messages = append(detail.Messages, dtos.TicketMessage{Sender: messages[i].SenderType, Body: messages[i].Body, CreatedAt: messages[i].CreatedAt})
	}
	return detail, nil
}

func (u *Usecase) ownTicket(ctx context.Context, userID, ticketID uuid.UUID) (*entities.SupportTicket, error) {
	ticket, err := u.Support.Find(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil || ticket.UserID != userID {
		return nil, entities.ErrTicketNotFound
	}
	return ticket, nil
}

func (u *Usecase) MyTicket(ctx context.Context, userID, ticketID uuid.UUID) (*dtos.TicketDetail, error) {
	ticket, err := u.ownTicket(ctx, userID, ticketID)
	if err != nil {
		return nil, err
	}
	return u.ticketDetail(ctx, ticket)
}

func toTicketList(tickets []entities.SupportTicket, total int64, page pagination.Params) *dtos.TicketList {
	list := &dtos.TicketList{Items: make([]dtos.TicketView, 0, len(tickets)), Total: total, Page: page.Page, Size: page.Size}
	for i := range tickets {
		list.Items = append(list.Items, toTicketView(&tickets[i]))
	}
	return list
}

func (u *Usecase) MyTickets(ctx context.Context, userID uuid.UUID, page pagination.Params) (*dtos.TicketList, error) {
	page = page.Normalize()
	tickets, total, err := u.Support.OfUser(ctx, userID, int(page.Limit()), int(page.Offset()))
	if err != nil {
		return nil, err
	}
	return toTicketList(tickets, total, page), nil
}

func (u *Usecase) reply(ctx context.Context, ticket *entities.SupportTicket, sender string, senderID uuid.UUID, text, status string) (*dtos.TicketDetail, error) {
	_, body, err := entities.NormalizeTicketText(ticket.Subject, text)
	if err != nil {
		return nil, err
	}
	if ticket.Status == entities.TicketClosed {
		return nil, entities.ErrTicketClosed
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Support.AddMessage(ctx, &entities.SupportMessage{TicketID: ticket.ID, SenderType: sender, SenderID: senderID, Body: body, CreatedAt: u.now()}); err != nil {
			return err
		}
		ticket.Status = status
		return u.Support.SetStatus(ctx, ticket.ID, status)
	})
	if err != nil {
		return nil, err
	}
	return u.ticketDetail(ctx, ticket)
}

func (u *Usecase) ReplyAsUser(ctx context.Context, userID, ticketID uuid.UUID, in dtos.TicketReplyInput) (*dtos.TicketDetail, error) {
	ticket, err := u.ownTicket(ctx, userID, ticketID)
	if err != nil {
		return nil, err
	}
	return u.reply(ctx, ticket, entities.TicketSenderUser, userID, in.Message, entities.TicketOpen)
}

func (u *Usecase) AdminTickets(ctx context.Context, status string, page pagination.Params) (*dtos.TicketList, error) {
	page = page.Normalize()
	tickets, total, err := u.Support.List(ctx, status, int(page.Limit()), int(page.Offset()))
	if err != nil {
		return nil, err
	}
	return toTicketList(tickets, total, page), nil
}

func (u *Usecase) adminTicket(ctx context.Context, id uuid.UUID) (*entities.SupportTicket, error) {
	ticket, err := u.Support.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, entities.ErrTicketNotFound
	}
	return ticket, nil
}

func (u *Usecase) AdminTicket(ctx context.Context, id uuid.UUID) (*dtos.TicketDetail, error) {
	ticket, err := u.adminTicket(ctx, id)
	if err != nil {
		return nil, err
	}
	return u.ticketDetail(ctx, ticket)
}

func (u *Usecase) ReplyAsStaff(ctx context.Context, actor entities.Actor, id uuid.UUID, in dtos.TicketReplyInput) (*dtos.TicketDetail, error) {
	ticket, err := u.adminTicket(ctx, id)
	if err != nil {
		return nil, err
	}
	sender := uuid.Nil
	if actor.ID != nil {
		sender = *actor.ID
	}
	detail, err := u.reply(ctx, ticket, entities.TicketSenderStaff, sender, in.Message, entities.TicketAnswered)
	if err != nil {
		return nil, err
	}
	if u.Inbox != nil {
		if err := u.Inbox.TicketAnswered(ctx, ticket.UserID, ticket.ID, ticket.Code); err != nil {
			return nil, err
		}
	}
	return detail, nil
}

func (u *Usecase) CloseTicket(ctx context.Context, actor entities.Actor, id uuid.UUID) (*dtos.TicketDetail, error) {
	ticket, err := u.adminTicket(ctx, id)
	if err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		ticket.Status = entities.TicketClosed
		if err := u.Support.SetStatus(ctx, id, entities.TicketClosed); err != nil {
			return err
		}
		return u.audit(ctx, actor, "support_ticket.close", "support_ticket", id.String(), nil)
	})
	if err != nil {
		return nil, err
	}
	return u.ticketDetail(ctx, ticket)
}
