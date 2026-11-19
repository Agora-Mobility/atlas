package experience

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/experience/dtos"
)

const (
	inboxKindTicket   = "support"
	inboxKindReferral = "referral"
	linkTicket        = "ticket"
	linkWallet        = "wallet"
	maxCodeAttempts   = 5
)

func toPreferencesView(p *entities.Preferences) *dtos.PreferencesView {
	navigation := p.NavigationApp
	return &dtos.PreferencesView{
		NotifyStatus: p.NotifyStatus, NotifyPromotions: p.NotifyPromotions, NotifySMS: p.NotifySMS, ShareLocation: p.ShareLocation,
		Language: p.Language, Currency: p.Currency, NavigationApp: &navigation,
	}
}

func (u *Usecase) Preferences(ctx context.Context, userID uuid.UUID) (*dtos.PreferencesView, error) {
	preferences, err := u.Account.Preferences(ctx, userID)
	if err != nil {
		return nil, err
	}
	if preferences == nil {
		return nil, entities.ErrInboxItemNotFound
	}
	return toPreferencesView(preferences), nil
}

func (u *Usecase) SavePreferences(ctx context.Context, userID uuid.UUID, in dtos.PreferencesView) (*dtos.PreferencesView, error) {
	preferences := &entities.Preferences{NotifyStatus: in.NotifyStatus, NotifyPromotions: in.NotifyPromotions, NotifySMS: in.NotifySMS, ShareLocation: in.ShareLocation, Language: in.Language, Currency: in.Currency}
	if in.NavigationApp != nil {
		preferences.NavigationApp = *in.NavigationApp
	} else {
		current, err := u.Account.Preferences(ctx, userID)
		if err != nil {
			return nil, err
		}
		if current != nil {
			preferences.NavigationApp = current.NavigationApp
		}
	}
	if err := preferences.Validate(); err != nil {
		return nil, err
	}
	if _, err := u.Account.SavePreferences(ctx, userID, preferences); err != nil {
		return nil, err
	}
	return toPreferencesView(preferences), nil
}

func (u *Usecase) SetAvatar(ctx context.Context, userID uuid.UUID, in dtos.AvatarInput) (*dtos.AvatarView, error) {
	if _, err := u.CheckPhotos(ctx, userID, []uuid.UUID{in.PhotoID}, entities.SettingMaxPhotos, 0); err != nil {
		return nil, err
	}
	if err := u.Account.SetAvatar(ctx, userID, in.PhotoID); err != nil {
		return nil, err
	}
	return u.Avatar(ctx, userID)
}

func (u *Usecase) Avatar(ctx context.Context, userID uuid.UUID) (*dtos.AvatarView, error) {
	key, err := u.Account.Avatar(ctx, userID)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return &dtos.AvatarView{}, nil
	}
	signed, err := u.Storage.PresignDownload(ctx, key)
	if err != nil {
		return nil, err
	}
	return &dtos.AvatarView{URL: signed.URL, ExpiresAt: &signed.ExpiresAt}, nil
}

func (u *Usecase) referralCode(ctx context.Context, userID uuid.UUID) (string, error) {
	code, err := u.Account.ReferralCode(ctx, userID)
	if err != nil || code != "" {
		return code, err
	}
	for range maxCodeAttempts {
		candidate, err := entities.NewReferralCode()
		if err != nil {
			return "", err
		}
		taken, err := u.Account.UserByReferralCode(ctx, candidate)
		if err != nil {
			return "", err
		}
		if taken != nil {
			continue
		}
		if err := u.Account.SetReferralCode(ctx, userID, candidate); err != nil {
			continue
		}
		return u.Account.ReferralCode(ctx, userID)
	}
	return "", errors.New("could not create a referral code")
}

func (u *Usecase) Referral(ctx context.Context, userID uuid.UUID) (*dtos.ReferralView, error) {
	code, err := u.referralCode(ctx, userID)
	if err != nil {
		return nil, err
	}
	reward, err := u.settingInt(ctx, entities.SettingReferralRewardCents)
	if err != nil {
		return nil, err
	}
	stats, err := u.Account.ReferralStats(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &dtos.ReferralView{Code: code, RewardCents: reward, Invited: stats.Total, Rewarded: stats.Rewarded, EarnedCents: stats.EarnedCents}, nil
}

func (u *Usecase) RedeemReferral(ctx context.Context, userID uuid.UUID, in dtos.RedeemReferralInput) error {
	code := strings.ToUpper(strings.TrimSpace(in.Code))
	referrer, err := u.Account.UserByReferralCode(ctx, code)
	if err != nil {
		return err
	}
	if referrer == nil {
		return entities.ErrReferralUnknown
	}
	if *referrer == userID {
		return entities.ErrReferralOwnCode
	}
	paid, err := u.Account.HasPaidRequest(ctx, userID)
	if err != nil {
		return err
	}
	if paid {
		return entities.ErrReferralTooLate
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	added, err := u.Account.AddReferral(ctx, id, *referrer, userID, u.now())
	if err != nil {
		return err
	}
	if !added {
		return entities.ErrReferralUsed
	}
	return nil
}

// OnFirstPaidRequest pays the invitation once: the friend has to complete the
// request and pay it. It runs inside the transaction of the payment.
func (u *Usecase) OnFirstPaidRequest(ctx context.Context, request *entities.ServiceRequest, now time.Time) error {
	if request.Status != entities.RequestCompleted {
		return nil
	}
	reward, err := u.settingInt(ctx, entities.SettingReferralRewardCents)
	if err != nil || reward <= 0 {
		return err
	}
	referralID, referrerID, err := u.Account.ClaimReward(ctx, request.CustomerID, reward, now)
	if err != nil || referrerID == nil {
		return err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	transaction := &entities.LedgerTransaction{ID: id, IdempotencyKey: "referral:" + referralID.String(), Kind: entities.LedgerReferralReward, RequestID: &request.ID, Currency: request.Fare.Currency, Note: "Convite", Legs: entities.ReferralLegs(*referrerID, reward), CreatedAt: now}
	if _, err := u.Ledger.Post(ctx, transaction); err != nil {
		return err
	}
	return u.AddInbox(ctx, &entities.InboxItem{UserID: *referrerID, Kind: inboxKindReferral, Title: "Ganhaste um prémio", Body: "O teu amigo concluiu o primeiro pedido. O prémio está na Carteira.", LinkType: linkWallet, EventID: "referral:" + referralID.String()})
}

func (u *Usecase) AddInbox(ctx context.Context, item *entities.InboxItem) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	item.ID, item.CreatedAt = id, u.now()
	return u.Account.AddInboxItem(ctx, item)
}

func (u *Usecase) TicketAnswered(ctx context.Context, userID, ticketID uuid.UUID, code string) error {
	return u.AddInbox(ctx, &entities.InboxItem{UserID: userID, Kind: inboxKindTicket, Title: "Resposta do apoio", Body: "O apoio respondeu ao ticket " + code + ".", LinkType: linkTicket, LinkID: &ticketID, EventID: "ticket:" + ticketID.String() + ":" + u.now().Format(time.RFC3339Nano)})
}

func (u *Usecase) ListInbox(ctx context.Context, userID uuid.UUID, onlyUnread bool, page pagination.Params) (*dtos.InboxList, error) {
	page = page.Normalize()
	items, total, unread, err := u.Account.Inbox(ctx, userID, onlyUnread, int(page.Limit()), int(page.Offset()))
	if err != nil {
		return nil, err
	}
	list := &dtos.InboxList{Items: make([]dtos.InboxItemView, 0, len(items)), Total: total, Unread: unread, Page: page.Page, Size: page.Size}
	for i := range items {
		list.Items = append(list.Items, dtos.InboxItemView{ID: items[i].ID, Kind: items[i].Kind, Title: items[i].Title, Body: items[i].Body, LinkType: items[i].LinkType, LinkID: items[i].LinkID, ReadAt: items[i].ReadAt, CreatedAt: items[i].CreatedAt})
	}
	return list, nil
}

func (u *Usecase) MarkInboxRead(ctx context.Context, userID, id uuid.UUID) error {
	found, err := u.Account.MarkRead(ctx, userID, id, u.now())
	if err != nil {
		return err
	}
	if !found {
		return entities.ErrInboxItemNotFound
	}
	return nil
}

func (u *Usecase) MarkAllInboxRead(ctx context.Context, userID uuid.UUID) (*dtos.MarkedView, error) {
	marked, err := u.Account.MarkAllRead(ctx, userID, u.now())
	if err != nil {
		return nil, err
	}
	return &dtos.MarkedView{Marked: marked}, nil
}

func (u *Usecase) Help(ctx context.Context, userID uuid.UUID, search string) (*dtos.HelpView, error) {
	topics, err := u.Account.HelpTopics(ctx, strings.TrimSpace(search))
	if err != nil {
		return nil, err
	}
	phone, err := u.Reference.Setting(ctx, entities.SettingSupportPhone)
	if err != nil {
		return nil, err
	}
	view := &dtos.HelpView{Topics: make([]dtos.HelpTopicView, 0, len(topics)), SupportPhone: phone}
	for i := range topics {
		view.Topics = append(view.Topics, dtos.HelpTopicView{ID: topics[i].ID, Code: topics[i].Code, Title: topics[i].Label, Body: topics[i].Description})
	}
	recent, err := u.Account.RecentRequest(ctx, userID)
	if err != nil {
		return nil, err
	}
	if recent != nil {
		view.RecentRequest = &dtos.RecentRequestView{ID: recent.ID, Code: recent.Code, Category: recent.Category, Status: recent.Status}
	}
	return view, nil
}
