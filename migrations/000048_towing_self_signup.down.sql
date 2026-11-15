UPDATE service_provider_requirements r
SET self_signup = false
FROM services s
WHERE r.service_id = s.id AND s.slug = 'towing';
