-- Província de Luanda como zona de serviço, para que qualquer ambiente (produção incluída) atenda Luanda
-- sem depender do seed. Limite da província no OpenStreetMap (relação 1802546, © contribuidores do
-- OpenStreetMap, ODbL), recortado a sul de -9.37 e a leste de 13.55 para tirar o que a Lei 14/24 passou
-- para Icolo e Bengo (Catete, Quiçama, Cabo Ledo). O aeroporto AIAAN fica dentro de propósito: as
-- recolhas lá têm de ser atendidas. Simplificado a ~200 m; um administrador afina pela API de zonas.
-- Reutiliza o id que o seed usava, por isso substitui o retângulo aproximado onde ele já exista.
INSERT INTO service_zones (id, country_code, name, geom, timezone, currency)
VALUES ('00000000-0000-0000-0000-0000000000c1', 'AO', 'Luanda',
        ST_GeogFromText('SRID=4326;MULTIPOLYGON(((12.927492 -9.37,12.903583 -9.325589,12.875428 -9.28591,12.856099 -9.246022,12.829482 -9.212318,12.814557 -9.186419,12.799891 -9.14955,12.790568 -9.09784,12.790658 -9.05117,12.794882 -9.011418,12.805949 -8.965728,12.826113 -8.924495,12.858374 -8.88552,12.888806 -8.837918,12.991204 -8.731729,13.106621 -8.62785,13.136216 -8.580024,13.398138 -8.622624,13.476647 -8.676269,13.496361 -8.665839,13.503364 -8.657444,13.522071 -8.64904,13.55 -8.704314,13.55 -9.37,12.927492 -9.37)))'),
        'Africa/Luanda', 'AOA')
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, geom = EXCLUDED.geom, is_active = true, updated_at = now();
