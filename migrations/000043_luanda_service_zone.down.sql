-- A zona só sai se ainda nada a usa; com pedidos ou tarifas ligados fica, porque apagá-la perderia histórico.
DELETE FROM service_zones z
WHERE z.id = '00000000-0000-0000-0000-0000000000c1'
  AND NOT EXISTS (SELECT 1 FROM service_requests r WHERE r.zone_id = z.id)
  AND NOT EXISTS (SELECT 1 FROM service_tariffs t WHERE t.zone_id = z.id)
  AND NOT EXISTS (SELECT 1 FROM dispatch_policies p WHERE p.zone_id = z.id);
