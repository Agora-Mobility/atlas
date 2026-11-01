# Changelog

## [1.2.0](https://github.com/Agora-Mobility/atlas/compare/v1.1.1...v1.2.0) (2026-11-01)


### Features

* **catalog:** add catalog entities, events and repository ([733a72d](https://github.com/Agora-Mobility/atlas/commit/733a72dad281fab21f3dd9a8d05ee00e0c9a255c))
* **catalog:** add the service catalog schema with the MVP categories ([badd510](https://github.com/Agora-Mobility/atlas/commit/badd51021afd9bed991b3555cae4e211d88bcbcc))
* **catalog:** manage the catalog in the backoffice and list it in the app ([ac8d806](https://github.com/Agora-Mobility/atlas/commit/ac8d806de0db970eafdb1223ea888c46834e7f60))

## [1.1.1](https://github.com/Agora-Mobility/atlas/compare/v1.1.0...v1.1.1) (2026-10-31)


### Bug Fixes

* **ci:** use the image healthcheck in the production deploy ([8e1e45d](https://github.com/Agora-Mobility/atlas/commit/8e1e45dcccf0412a247004d46e085b03d10ef25c))
* **ci:** use the image healthcheck in the production deploy ([ae5f9d5](https://github.com/Agora-Mobility/atlas/commit/ae5f9d54aec4b7462ca85eee3bfdb0e4386fd44a))

## [1.1.0](https://github.com/Agora-Mobility/atlas/compare/v1.0.0...v1.1.0) (2026-10-30)


### Features

* **auth:** add multi-scope RBAC with MFA, row-level security and audit trail ([cf60fb4](https://github.com/Agora-Mobility/atlas/commit/cf60fb45b647d19bb1b562e5c66c47e1be6ca15a))
* **auth:** confirm e-mail changes and account deletion with an SMS code ([b193175](https://github.com/Agora-Mobility/atlas/commit/b193175820f9995e6eed25d54a2440b1428c0069))
* **auth:** sign customers and providers in with a phone code ([c846936](https://github.com/Agora-Mobility/atlas/commit/c846936b1138531618a4643eb9f4f6f689187f3d))
* **notifications:** add a Resend e-mail sender ([2701721](https://github.com/Agora-Mobility/atlas/commit/27017212720be51cd3dfc30b4edc14e51a632563))
* **notifications:** send invitations and access notices by e-mail ([67d2ce8](https://github.com/Agora-Mobility/atlas/commit/67d2ce8964c9dc676724e64089b71e889cccb034))
* **partners:** let AGORA update the name and NIF of a partner ([96da673](https://github.com/Agora-Mobility/atlas/commit/96da673f2fda9efc39266db83b2dda542491880a))

## 1.0.0 (2026-10-28)


### Features

* **auth:** authentication flow with client identification ([a49a2a1](https://github.com/Agora-Mobility/atlas/commit/a49a2a1e9bd0bbd00bd93569a29b260642ed96be))
* **auth:** authentication with client identification, CastBrick SMS and platform cleanup ([c3491c7](https://github.com/Agora-Mobility/atlas/commit/c3491c7e01ffb4ee02d6747860f07a2f3c15c0ce))
* **auth:** normalize phone numbers to E.164 with Angola as the default region ([df1cbac](https://github.com/Agora-Mobility/atlas/commit/df1cbaceffe37c0570b4bf0167007b5225e50298))
* **auth:** normalize phone numbers to E.164 with Angola as the default region ([9c5b609](https://github.com/Agora-Mobility/atlas/commit/9c5b609eb4ec040678ed9721ffaffe7c2ccf93f2))
* **db:** postgres, migrations, sqlc and redis ([6363fbb](https://github.com/Agora-Mobility/atlas/commit/6363fbbcaceff473c2cc285518a848a84ab144a5))
* **docs:** Scalar API reference only in development ([bc5ed67](https://github.com/Agora-Mobility/atlas/commit/bc5ed679d545859c8ba9df0f8275c8c0acfef216))
* **events:** keep unrouted events and send a welcome SMS ([29e275f](https://github.com/Agora-Mobility/atlas/commit/29e275fb9b9ee294c07b7287e7bd16c7cbffdf2c))
* **events:** rabbitmq, outbox relay, publisher and consumer ([b12836d](https://github.com/Agora-Mobility/atlas/commit/b12836df70fd99da2ffd06343e9c5631d3508611))
* **http:** gin server, middlewares, health and example module ([3af38cf](https://github.com/Agora-Mobility/atlas/commit/3af38cfec9fd29fbcebaf850c751a8731b12647d))
* **http:** streaming middleware for SSE routes ([1a71982](https://github.com/Agora-Mobility/atlas/commit/1a71982f68659d6949d3ca781bf3b088944002de))
* **identity:** users, clients and refresh tokens persistence ([1518011](https://github.com/Agora-Mobility/atlas/commit/15180116328c6509b1e84cc4b95d3193433ae9c3))
* **notifications:** drop the sender prefix and fit OTP texts in one SMS ([9671c8f](https://github.com/Agora-Mobility/atlas/commit/9671c8ff28afbff50faee494f47b93906b23a6a4))
* **notifications:** drop the sender prefix and fit OTP texts in one SMS ([ef9353f](https://github.com/Agora-Mobility/atlas/commit/ef9353f30631cc5878fbb7f63fd454b01fdf71e8))
* **security:** argon2id passwords, opaque refresh tokens and Redis OTP ([9ce9249](https://github.com/Agora-Mobility/atlas/commit/9ce9249d1d2e4bb0310e873fba004622689d0848))
* **sms:** send SMS through CastBrick ([d2882c6](https://github.com/Agora-Mobility/atlas/commit/d2882c6d7168586631fb5f1b93c7b3e886e26404))


### Bug Fixes

* **ci:** target main with release-please and build images even if it fails ([5fda148](https://github.com/Agora-Mobility/atlas/commit/5fda1489a13dafa25bee2de898376f0cefdbbca6))
* **ci:** target main with release-please and build images even if it fails ([2780157](https://github.com/Agora-Mobility/atlas/commit/27801578d7d3056875b8c6c6ea8c4880f5c235e6))
