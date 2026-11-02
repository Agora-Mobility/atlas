-- name: GetUserPreferences :one
SELECT notify_status, notify_promotions, notify_sms, share_location, language, currency FROM users WHERE id = $1 AND deleted_at IS NULL;

-- name: UpdateUserPreferences :execrows
UPDATE users SET notify_status = sqlc.arg(notify_status), notify_promotions = sqlc.arg(notify_promotions), notify_sms = sqlc.arg(notify_sms), share_location = sqlc.arg(share_location), language = sqlc.arg(language), currency = sqlc.arg(currency), updated_at = now()
WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- name: SetUserAvatar :exec
UPDATE users SET avatar_upload_id = $2, updated_at = now() WHERE id = $1;

-- name: GetUserAvatar :one
SELECT u.avatar_upload_id, COALESCE(m.object_key, '')::text AS object_key FROM users u LEFT JOIN media_uploads m ON m.id = u.avatar_upload_id WHERE u.id = $1;

-- name: GetReferralCode :one
SELECT referral_code FROM users WHERE id = $1;

-- name: SetReferralCode :exec
UPDATE users SET referral_code = $2 WHERE id = $1 AND referral_code IS NULL;

-- name: FindUserByReferralCode :one
SELECT id FROM users WHERE referral_code = $1 AND deleted_at IS NULL;

-- name: InsertReferral :execrows
INSERT INTO referrals (id, referrer_id, referred_id, status, created_at) VALUES ($1, $2, $3, 'pending', $4) ON CONFLICT (referred_id) DO NOTHING;

-- name: ClaimReferralReward :one
UPDATE referrals SET status = 'rewarded', reward_cents = sqlc.arg(reward_cents), rewarded_at = sqlc.arg(rewarded_at)
WHERE referred_id = sqlc.arg(referred_id) AND status = 'pending' RETURNING id, referrer_id;

-- name: CountReferrals :one
SELECT count(*) AS total, count(*) FILTER (WHERE status = 'rewarded') AS rewarded, COALESCE(sum(reward_cents), 0)::bigint AS earned_cents FROM referrals WHERE referrer_id = $1;

-- name: HasPaidRequest :one
SELECT EXISTS (SELECT 1 FROM service_requests WHERE customer_id = $1 AND payment_status = 'paid');

-- name: InsertInboxItem :exec
INSERT INTO inbox_items (id, user_id, kind, title, body, link_type, link_id, event_id, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (user_id, event_id) DO NOTHING;

-- name: ListInboxItems :many
SELECT * FROM inbox_items WHERE user_id = $1 AND (NOT sqlc.arg(only_unread)::boolean OR read_at IS NULL)
ORDER BY created_at DESC, id DESC LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountInboxItems :one
SELECT count(*) AS total, count(*) FILTER (WHERE read_at IS NULL) AS unread FROM inbox_items WHERE user_id = $1;

-- name: MarkInboxItemRead :execrows
UPDATE inbox_items SET read_at = COALESCE(read_at, sqlc.arg(at)) WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: MarkAllInboxRead :execrows
UPDATE inbox_items SET read_at = sqlc.arg(at) WHERE user_id = sqlc.arg(user_id) AND read_at IS NULL;

-- name: SearchHelpTopics :many
SELECT * FROM catalog_items WHERE kind = 'help_topic' AND is_active AND (sqlc.arg(search)::text = '' OR label ILIKE '%' || sqlc.arg(search)::text || '%' OR description ILIKE '%' || sqlc.arg(search)::text || '%')
ORDER BY display_order;

-- name: RecentRequestOfUser :one
SELECT id, public_code, category_slug, status FROM service_requests WHERE customer_id = $1 ORDER BY created_at DESC LIMIT 1;
