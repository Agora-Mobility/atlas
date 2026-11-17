# Changelog

## [3.5.0](https://github.com/Agora-Mobility/atlas/compare/v3.4.0...v3.5.0) (2026-11-17)


### Features

* **notifications:** tell the driver about cancellations, assignments and route changes ([28271f2](https://github.com/Agora-Mobility/atlas/commit/28271f243149a377e9aedd6dc0a3cb4edebff96d))
* **payments:** turn off digital payments and cancellation fees for customers ([b853bea](https://github.com/Agora-Mobility/atlas/commit/b853bea7a3986e918c4e68c9c4d27dca0d8f6533))


### Bug Fixes

* **bookings:** default the payment method to cash ([24c8d44](https://github.com/Agora-Mobility/atlas/commit/24c8d44310fd90403f0fff0e2c031095e694691a))
* **deps:** restore go.sum after the aws-sdk dependabot merges ([49f91b0](https://github.com/Agora-Mobility/atlas/commit/49f91b0fbf9f566e0f74888dd5231874ca6b1e9c))
* **notifications:** no repeated SMS, and the driver notifications that were missing ([a0227b3](https://github.com/Agora-Mobility/atlas/commit/a0227b3d4c6a24295d9658f616abe29cb05f879a))
* **notifications:** send each push and SMS once per event ([3475a8d](https://github.com/Agora-Mobility/atlas/commit/3475a8d0380334caac89e4296fe5e9d52fb108bf))
* **payments:** stop blocking new requests on unpaid ones ([dd47c88](https://github.com/Agora-Mobility/atlas/commit/dd47c883e3ffee7e24bda3668381ae8017831f4e))
* **push:** stop retrying pushes that FCM rejects for good ([1fc3b4d](https://github.com/Agora-Mobility/atlas/commit/1fc3b4d7250d16a6faffee1672f9036cff7e2aed))

## [3.4.0](https://github.com/Agora-Mobility/atlas/compare/v3.3.0...v3.4.0) (2026-11-16)


### Features

* **dispatch:** cancel a trip in progress (customer, driver and operator) ([b414326](https://github.com/Agora-Mobility/atlas/commit/b4143265082e31ac73f7adadb2fa1a8e5984f135))
* **dispatch:** customer and driver can cancel a trip in progress too ([1d4a37c](https://github.com/Agora-Mobility/atlas/commit/1d4a37c80f3cb2484583df609248eeb14d890da3))
* **dispatch:** let an operator cancel a trip in progress ([ac2280d](https://github.com/Agora-Mobility/atlas/commit/ac2280dd3324e0d7ad1e5f0d6a8faa6f5d066245))

## [3.3.0](https://github.com/Agora-Mobility/atlas/compare/v3.2.1...v3.3.0) (2026-11-16)


### Features

* **dispatch:** aggregate requests into H3 heatmap cells ([c8293f6](https://github.com/Agora-Mobility/atlas/commit/c8293f6d486a2162f3a8a1398a58f4e88687a670))
* **dispatch:** backfill the pickup cells of older requests ([da5be77](https://github.com/Agora-Mobility/atlas/commit/da5be777c4754c821a302298e7c1fbabe968336d))
* **dispatch:** H3 heatmap cells for the backoffice map ([9d87b7f](https://github.com/Agora-Mobility/atlas/commit/9d87b7feac44acfe02f766d864f1842e00e13be4))
* **dispatch:** keep the resolution 9 pickup cell on each request ([49dddc0](https://github.com/Agora-Mobility/atlas/commit/49dddc0221fed696f34902213953cf23652353a1))
* **dispatch:** serve the heatmap cells of a map view ([05ffcbc](https://github.com/Agora-Mobility/atlas/commit/05ffcbcb3a788aaba6b2b9d23aa3ce1f465a4554))
* **geo:** list the H3 cells of a map view ([c910072](https://github.com/Agora-Mobility/atlas/commit/c910072822806478f4172caa2dcbdc4257818e23))
* **notifications:** push chat messages to the recipient ([822bc43](https://github.com/Agora-Mobility/atlas/commit/822bc43ad932d34b8bd3c055234bee98f03d15c8))
* **payments:** find the request a customer still has to pay ([d724634](https://github.com/Agora-Mobility/atlas/commit/d72463402b2ad2bc95751d1ba177aade34243deb))
* **providers:** open towing to provider sign-up ([123de3a](https://github.com/Agora-Mobility/atlas/commit/123de3aadd36818a0f9727cdfb86eaa6cee2c900))
* **providers:** open towing to provider sign-up ([2c4e21d](https://github.com/Agora-Mobility/atlas/commit/2c4e21d47549b58b9b221085aa866d7e668189a8))
* **removals:** filter items by property and price items added by hand ([5690ffb](https://github.com/Agora-Mobility/atlas/commit/5690ffb67d2ece4c795e90374c8dce919970f4aa))
* **removals:** filter the items by type of property ([9635c9e](https://github.com/Agora-Mobility/atlas/commit/9635c9ec3365141127cdb62bc6bfa04a7a038c49))
* **removals:** price items the customer adds by hand ([4b6353c](https://github.com/Agora-Mobility/atlas/commit/4b6353c1b93f10d88af77d536badfaf1008fa81e))
* **workshops:** partners set the gallery and the app shows it ([b41855d](https://github.com/Agora-Mobility/atlas/commit/b41855df09d4d88d74f62fbc850fb75ffe68ace9))
* **workshops:** photo gallery with a cover ([353c2c7](https://github.com/Agora-Mobility/atlas/commit/353c2c77a1340e85611c935c2cc9bb5a91ac65ee))
* **workshops:** store an ordered gallery of photos per workshop ([f1b3318](https://github.com/Agora-Mobility/atlas/commit/f1b33180a1c42d3cafa86481e24910ad3328a63e))


### Bug Fixes

* **payments:** tell the app which request blocks a new one ([9633e80](https://github.com/Agora-Mobility/atlas/commit/9633e80beb70a6a40a7be42abd890f4acc443be4))
* **payments:** tell the app which request blocks a new one ([c256140](https://github.com/Agora-Mobility/atlas/commit/c256140fbd5397c00af449dbb6a77e9bf5b5149d))

## [3.2.1](https://github.com/Agora-Mobility/atlas/compare/v3.2.0...v3.2.1) (2026-11-11)


### Bug Fixes

* **dispatch:** build the tests again after merging demand pricing and heatmap ([6a930c4](https://github.com/Agora-Mobility/atlas/commit/6a930c470ca587cc0792e40083c31dd652e8db16))

## [3.2.0](https://github.com/Agora-Mobility/atlas/compare/v3.1.0...v3.2.0) (2026-11-11)


### Features

* **dispatch:** heatmap filters, origin-destination flows and provider demand ([10eb25a](https://github.com/Agora-Mobility/atlas/commit/10eb25a89d18f4268cfd47a046cb71d841d854b1))
* **dispatch:** heatmap filters, origin-destination flows and provider demand ([a173afc](https://github.com/Agora-Mobility/atlas/commit/a173afc5250053a01eef3c9c75dfe61a82354c2b))
* **dispatch:** record trip deviation against the quote ([2dc68fd](https://github.com/Agora-Mobility/atlas/commit/2dc68fd4f7b738acaccc73bd7a84fc16d652bcaf))
* **geo:** look up H3 parent and children cells ([69ef837](https://github.com/Agora-Mobility/atlas/commit/69ef837fe4911460b87c58b2a5d311ec6ed58451))
* **partners:** get one partner by id ([f654880](https://github.com/Agora-Mobility/atlas/commit/f6548802093f03b60d06d714c03ae2848978e4f4)), closes [#42](https://github.com/Agora-Mobility/atlas/issues/42)
* **partners:** get one partner by id. ([88492ae](https://github.com/Agora-Mobility/atlas/commit/88492aecc61cd0e479c58b7bb7a36dced5311bf8))
* **pricing:** demand pricing and trip deviation (proposal 0002) ([5c92e37](https://github.com/Agora-Mobility/atlas/commit/5c92e37802f526161b7c9832dd43059dd369d5e4))
* **pricing:** raise closed fares by zone demand ([80a930e](https://github.com/Agora-Mobility/atlas/commit/80a930e69460ea23d2c1a99a06b2fb2bc50f5e11))

## [3.1.0](https://github.com/Agora-Mobility/atlas/compare/v3.0.0...v3.1.0) (2026-11-08)


### Features

* **dispatch:** give the quote route once for the list ([9aa2241](https://github.com/Agora-Mobility/atlas/commit/9aa2241f395aa0a6283a6deb9ec72d058f6ba32f))
* **dispatch:** return the road route geometry in quotes and requests ([80ee238](https://github.com/Agora-Mobility/atlas/commit/80ee238bacd5f08a39cb5c38141f24a8b3659d7a))
* **zones:** serve the whole Luanda province and seed workshops in Futungo ([c39b91f](https://github.com/Agora-Mobility/atlas/commit/c39b91f1e74e6e7754b4ece1aaebb441f1380048))


### Bug Fixes

* **deps:** bump golang.org/x/net to v0.62.0 for CVE-2026-78661 ([f54f64f](https://github.com/Agora-Mobility/atlas/commit/f54f64fa073f45cc81959095334f48b2f1ae1255))

## [3.0.0](https://github.com/Agora-Mobility/atlas/compare/v2.0.0...v3.0.0) (2026-11-06)


### ⚠ BREAKING CHANGES

* **auth:** POST /auth/recovery/confirm now takes recovery_token and new_password, instead of phone, code and new_password.
* **routing:** route_source and source answer google instead of mapbox, and route.geometry is now an encoded polyline of precision 5, not 6.

### Features

* **auth:** add a single-use ticket for password recovery ([f49ee32](https://github.com/Agora-Mobility/atlas/commit/f49ee32c08253f1731a64e6dd0875e5c4c8b458b))
* **auth:** recover the password by e-mail or SMS in two steps ([1714caf](https://github.com/Agora-Mobility/atlas/commit/1714caf0a0f528a75ec373384d957f2d61b66650))
* **auth:** remove MFA, every account signs in with phone and password alone ([8a7e585](https://github.com/Agora-Mobility/atlas/commit/8a7e585064a3a49312b8b321f3fad0815c327f02))
* **auth:** remove MFA, two-step password recovery and Google Maps routing ([7a76acc](https://github.com/Agora-Mobility/atlas/commit/7a76acc234fa404e4c3bcb867997b5fe2330ab15))
* **config:** read the Firebase service account from FCM_CREDENTIALS_FILE ([cc4472c](https://github.com/Agora-Mobility/atlas/commit/cc4472c72de294218bed08a5052c6433e8a72969))
* **emails:** add the password recovery code message ([a126e3b](https://github.com/Agora-Mobility/atlas/commit/a126e3bae7b316a675295563d826bbc9a6b06b89))
* **notifications:** tell providers about their verification ([6cf05dc](https://github.com/Agora-Mobility/atlas/commit/6cf05dc7c8f3e4263d30db23b0bf626a8fac18b4))
* P2 Repeat flow, bug fixes, swagger, tests, docs ([3215db9](https://github.com/Agora-Mobility/atlas/commit/3215db9d3aa80050a770e797f0c421d76e754f8f))
* **pricing:** accept google as the route source of a quote ([d58d857](https://github.com/Agora-Mobility/atlas/commit/d58d857b250da7c1df9844ed8de671b7b4f097e5))
* **providers:** add bank and insurer catalogs, rules per service and reminders ([a90498c](https://github.com/Agora-Mobility/atlas/commit/a90498cfc6836d86357981c29acc3892116d967c))
* **providers:** add the image quality, OCR and liveness ports ([5e77eb2](https://github.com/Agora-Mobility/atlas/commit/5e77eb2aa680cc45b75f1249b0dd0899a048200a))
* **providers:** add the rules that check IBAN, plate, NIF, BI, names and dates ([bfc379a](https://github.com/Agora-Mobility/atlas/commit/bfc379a0c346f7692a6cd4bd2ca1cbe9a8283488))
* **providers:** add the verification endpoints of AGORA Pro ([f631494](https://github.com/Agora-Mobility/atlas/commit/f631494e1cbad966ebe27a7fd249335019f80e2c))
* **providers:** AGORA Pro provider verification (KYC) ([050ba74](https://github.com/Agora-Mobility/atlas/commit/050ba7497b244ee9f76c8365cc8803a9425c1428))
* **providers:** compute the verification progress on the server ([5ba2623](https://github.com/Agora-Mobility/atlas/commit/5ba262390b169cd83a09a8669bfbcc0db889c7b2))
* **providers:** keep document types, slots and subjects as enums ([6e5fada](https://github.com/Agora-Mobility/atlas/commit/6e5fadaa6ae6791e31318c812256cadbe7876e78))
* **providers:** take the service and personal data in one profile step ([c12a33e](https://github.com/Agora-Mobility/atlas/commit/c12a33e92117af8c30838bea14c5f947edcb9b48))
* **rest:** explain required_without and excluded_with errors ([d7e00b3](https://github.com/Agora-Mobility/atlas/commit/d7e00b3e7f7ed4078258db25d774f8f099634451))
* **routing:** replace Mapbox with the Google Routes API ([119d0bc](https://github.com/Agora-Mobility/atlas/commit/119d0bcbc0b856ab214eab5c175d194cfc78bfa0))
* **shared:** detect file types by their magic bytes, HEIC included ([bb99d94](https://github.com/Agora-Mobility/atlas/commit/bb99d94ccb7414ec6c8cbefa8e1ff419d5260d59))
* **shared:** strip EXIF and GPS from photos without re-encoding them ([b2e88a7](https://github.com/Agora-Mobility/atlas/commit/b2e88a7695dce1a3a5bb0bfa2b134a46e4498619))

## [2.0.0](https://github.com/Agora-Mobility/atlas/compare/v1.2.0...v2.0.0) (2026-11-03)


### ⚠ BREAKING CHANGES

* **auth:** POST /auth/login takes `phone` instead of `identifier` and ignores `organization_id`. Accounts without a phone, such as an admin made by the previous bootstrap-admin, cannot sign in until they get one.

### Features

* **auth:** add a separate sign-in with an SMS code ([e970c87](https://github.com/Agora-Mobility/atlas/commit/e970c8792e1bc8c0b111943e7ad24bd9047d6f65))
* **auth:** sign every account in with the phone and password ([82d75a9](https://github.com/Agora-Mobility/atlas/commit/82d75a960b41a46c6827b880154e2e1d8a6ab568))

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
