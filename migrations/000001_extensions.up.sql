-- Extensions stay in public; the application objects live in the atlas
-- schema (see 000007 and ADR 0014).
CREATE EXTENSION IF NOT EXISTS postgis WITH SCHEMA public;
