# Changelog

## [1.23.0](https://github.com/didit-protocol/sdk-go/compare/v1.22.0...v1.23.0) (2026-11-14)


### Features

* **spec:** sync API spec 2026-11-14 ([92bf6d0](https://github.com/didit-protocol/sdk-go/commit/92bf6d0b3656aab778009b936863412b8e19823a))

## [1.22.0](https://github.com/didit-protocol/sdk-go/compare/v1.21.0...v1.22.0) (2026-11-14)


### Features

* **spec:** sync API spec 2026-11-14 ([5c7ef60](https://github.com/didit-protocol/sdk-go/commit/5c7ef60515eae31e071dfcbc2a861f0777b667ca))

## [1.21.0](https://github.com/didit-protocol/sdk-go/compare/v1.20.0...v1.21.0) (2026-11-14)


### Features

* **spec:** sync API spec 2026-11-14 ([53b096f](https://github.com/didit-protocol/sdk-go/commit/53b096ff6fcef5a5442284a424f4bcd00a762e4e))

## [1.20.0](https://github.com/didit-protocol/sdk-go/compare/v1.19.0...v1.20.0) (2026-11-14)


### Features

* **spec:** sync API spec 2026-11-14 ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))


### API changes that can break typed code

* **spec:** `GET /v3/travel-rule/networks/` response `networks.gtr.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.gtr.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.gtr.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.gtr.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.gtr.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.gtr.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.sygna.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.sygna.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.sygna.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.sygna.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.sygna.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.sygna.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.trust.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.trust.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.trust.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.trust.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.trust.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.trust.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.verifyvasp.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.verifyvasp.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.verifyvasp.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.verifyvasp.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.verifyvasp.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/networks/` response `networks.verifyvasp.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.gtr.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.gtr.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.gtr.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.gtr.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.gtr.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.gtr.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.sygna.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.sygna.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.sygna.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.sygna.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.sygna.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.sygna.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.trust.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.trust.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.trust.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.trust.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.trust.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.trust.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.verifyvasp.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.verifyvasp.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.verifyvasp.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.verifyvasp.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.verifyvasp.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/settings/` response `networks.verifyvasp.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.gtr.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.gtr.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.gtr.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.gtr.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.gtr.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.gtr.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.sygna.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.sygna.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.sygna.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.sygna.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.sygna.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.sygna.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.trust.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.trust.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.trust.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.trust.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.trust.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.trust.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.verifyvasp.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.verifyvasp.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.verifyvasp.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.verifyvasp.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.verifyvasp.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `GET /v3/travel-rule/workspace/` response `settings.networks.verifyvasp.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.gtr.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.gtr.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.gtr.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.gtr.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.gtr.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.gtr.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.sygna.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.sygna.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.sygna.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.sygna.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.sygna.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.sygna.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.trust.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.trust.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.trust.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.trust.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.trust.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.trust.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.verifyvasp.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.verifyvasp.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.verifyvasp.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.verifyvasp.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.verifyvasp.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/billing/` response `settings.networks.verifyvasp.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.gtr.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.gtr.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.gtr.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.gtr.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.gtr.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.gtr.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.sygna.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.sygna.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.sygna.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.sygna.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.sygna.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.sygna.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.trust.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.trust.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.trust.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.trust.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.trust.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.trust.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.verifyvasp.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.verifyvasp.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.verifyvasp.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.verifyvasp.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.verifyvasp.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/claim/` response `settings.networks.verifyvasp.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.gtr.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.gtr.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.gtr.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.gtr.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.gtr.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.gtr.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.sygna.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.sygna.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.sygna.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.sygna.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.sygna.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.sygna.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.trust.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.trust.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.trust.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.trust.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.trust.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.trust.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.verifyvasp.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.verifyvasp.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.verifyvasp.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.verifyvasp.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.verifyvasp.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/confirm/` response `settings.networks.verifyvasp.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.gtr.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.gtr.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.gtr.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.gtr.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.gtr.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.gtr.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.sygna.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.sygna.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.sygna.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.sygna.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.sygna.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.sygna.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.trust.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.trust.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.trust.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.trust.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.trust.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.trust.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.verifyvasp.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.verifyvasp.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.verifyvasp.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.verifyvasp.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.verifyvasp.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `POST /v3/travel-rule/workspace/domain/dns/` response `settings.networks.verifyvasp.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.gtr.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.gtr.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.gtr.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.gtr.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.gtr.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.gtr.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.sygna.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.sygna.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.sygna.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.sygna.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.sygna.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.sygna.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.trust.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.trust.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.trust.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.trust.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.trust.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.trust.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.verifyvasp.connected`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.verifyvasp.platform_available`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.verifyvasp.reason`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.verifyvasp.routed_30d`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.verifyvasp.state`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))
* **spec:** `PUT /v3/travel-rule/settings/` response `networks.verifyvasp.vasps_reachable`: can now be null ([68dc639](https://github.com/didit-protocol/sdk-go/commit/68dc639b93114c390e930d99eaae900417c86db7))

## [1.19.0](https://github.com/didit-protocol/sdk-go/compare/v1.18.0...v1.19.0) (2026-11-13)


### Features

* **spec:** sync API spec 2026-11-13 ([5781f5b](https://github.com/didit-protocol/sdk-go/commit/5781f5b1947ebd8abdbd5fc1cdc7bc2cbf16165b))

## [1.18.0](https://github.com/didit-protocol/sdk-go/compare/v1.17.0...v1.18.0) (2026-11-13)


### Features

* **spec:** sync API spec 2026-11-13 ([8b0ae7f](https://github.com/didit-protocol/sdk-go/commit/8b0ae7f74e2bd1d973454eb5c21816f0cf08efdc))

## [1.17.0](https://github.com/didit-protocol/sdk-go/compare/v1.16.0...v1.17.0) (2026-11-12)


### Features

* **spec:** sync API spec 2026-11-12 ([68b0b74](https://github.com/didit-protocol/sdk-go/commit/68b0b7465855842c83e189802af2a170755a84b9))

## [1.16.0](https://github.com/didit-protocol/sdk-go/compare/v1.15.2...v1.16.0) (2026-11-12)


### Features

* **spec:** sync API spec 2026-11-12 ([ab42c28](https://github.com/didit-protocol/sdk-go/commit/ab42c28a487e540ba1db357b42d03e694b4d2ca2))

## [1.15.2](https://github.com/didit-protocol/sdk-go/compare/v1.15.1...v1.15.2) (2026-11-12)


### Bug Fixes

* **authentication:** redact the parsed agent-consent response structurally ([#63](https://github.com/didit-protocol/sdk-go/issues/63)) ([843a22d](https://github.com/didit-protocol/sdk-go/commit/843a22d0dcff6d348814202a6f468e9f8ef6da12))

## [1.15.1](https://github.com/didit-protocol/sdk-go/compare/v1.15.0...v1.15.1) (2026-11-11)


### Bug Fixes

* **authentication:** redact secrets before capping an error body ([bcdd44e](https://github.com/didit-protocol/sdk-go/commit/bcdd44edfea7b66b80cd1033513e6f1a53d304ed))

## [1.15.0](https://github.com/didit-protocol/sdk-go/compare/v1.14.0...v1.15.0) (2026-11-11)


### Features

* **spec:** sync API spec 2026-11-11 ([331783a](https://github.com/didit-protocol/sdk-go/commit/331783ab27adf8a23985a01d10bb45de947642d9))


### API changes that can break typed code

* **spec:** `POST /v3/organization/{organization_id}/application/{application_id}/cases/` request body `links[].entity_type`: added (required) ([331783a](https://github.com/didit-protocol/sdk-go/commit/331783ab27adf8a23985a01d10bb45de947642d9))
* **spec:** `POST /v3/organization/{organization_id}/application/{application_id}/cases/` request body `links[].entity_uuid`: added (required) ([331783a](https://github.com/didit-protocol/sdk-go/commit/331783ab27adf8a23985a01d10bb45de947642d9))
* **spec:** `POST /v3/organization/{organization_id}/application/{application_id}/cases/blueprints/{blueprint_uuid}/publish/` response `content_config`: type changed from any to object ([331783a](https://github.com/didit-protocol/sdk-go/commit/331783ab27adf8a23985a01d10bb45de947642d9))

## [1.14.0](https://github.com/didit-protocol/sdk-go/compare/v1.13.0...v1.14.0) (2026-11-11)


### Features

* **spec:** sync API spec 2026-11-11 ([daebcb5](https://github.com/didit-protocol/sdk-go/commit/daebcb55a6697195870f2dd99a14981dd937a392))

## [1.13.0](https://github.com/didit-protocol/sdk-go/compare/v1.12.0...v1.13.0) (2026-11-11)


### Features

* **spec:** sync API spec 2026-11-11 ([d50aef6](https://github.com/didit-protocol/sdk-go/commit/d50aef6703ddb99e364b68a0dc209ba366fa1f21))

## [1.12.0](https://github.com/didit-protocol/sdk-go/compare/v1.11.0...v1.12.0) (2026-11-11)


### Features

* **spec:** sync API spec 2026-11-11 ([ffe5731](https://github.com/didit-protocol/sdk-go/commit/ffe57313d7b1230136c4ec80ebdf97f87970bca3))
* **spec:** sync API spec 2026-11-11 ([c43076b](https://github.com/didit-protocol/sdk-go/commit/c43076b7b482d55c1865b13c04bbf9020e16d9b7))
* **spec:** sync API spec 2026-11-11 ([fafdc5b](https://github.com/didit-protocol/sdk-go/commit/fafdc5ba60388b2b66eb24b9206cf88ad03f4fc0))
* **spec:** sync API spec 2026-11-11 ([f15ffe8](https://github.com/didit-protocol/sdk-go/commit/f15ffe833c9ee2ed4aad66f13a40f44abdd794de))
* **spec:** sync API spec 2026-11-11 ([22f2021](https://github.com/didit-protocol/sdk-go/commit/22f2021efca24bc4a52cfe36935bd56af7d75054))
* **spec:** sync API spec 2026-11-11 ([e1baed7](https://github.com/didit-protocol/sdk-go/commit/e1baed7b4b2a999e05a0a80116aedddbafc0c546))
* **spec:** sync API spec 2026-11-11 ([f725553](https://github.com/didit-protocol/sdk-go/commit/f7255534f92cfa8e6b3fcdeac6ef87f5e75e8dc3))
* **spec:** sync API spec 2026-11-11 ([7973f33](https://github.com/didit-protocol/sdk-go/commit/7973f338f2fea7472fefa4e0a42de717380f5bf1))

## [1.11.0](https://github.com/didit-protocol/sdk-go/compare/v1.10.0...v1.11.0) (2026-11-11)


### Features

* **spec:** sync API spec 2026-11-11 ([b2651d6](https://github.com/didit-protocol/sdk-go/commit/b2651d65cb0a793ceeb5f71bfdad1359db69b0a9))

## [1.10.0](https://github.com/didit-protocol/sdk-go/compare/v1.9.0...v1.10.0) (2026-11-10)


### Features

* **spec:** sync API spec 2026-11-10 ([a62a765](https://github.com/didit-protocol/sdk-go/commit/a62a765ac1cc42c71850f12fe03f01335d931712))

## [1.9.0](https://github.com/didit-protocol/sdk-go/compare/v1.8.0...v1.9.0) (2026-11-10)


### Features

* **spec:** sync API spec 2026-11-10 ([4d6bc05](https://github.com/didit-protocol/sdk-go/commit/4d6bc05266976ea398d7d1e9be0fed39b85d06e5))

## [1.8.0](https://github.com/didit-protocol/sdk-go/compare/v1.7.0...v1.8.0) (2026-11-10)


### Features

* **spec:** sync API spec 2026-11-10 ([c8993d3](https://github.com/didit-protocol/sdk-go/commit/c8993d343566e0ccc0511bf9739c7b0434562adc))

## [1.7.0](https://github.com/didit-protocol/sdk-go/compare/v1.6.0...v1.7.0) (2026-11-10)


### Features

* **spec:** sync API spec 2026-11-10 ([0c6b4e2](https://github.com/didit-protocol/sdk-go/commit/0c6b4e2b22547fe4780580ac057c508d5e6867b1))

## [1.6.0](https://github.com/didit-protocol/sdk-go/compare/v1.5.0...v1.6.0) (2026-11-10)


### Features

* **spec:** sync API spec 2026-11-10 ([ec7847c](https://github.com/didit-protocol/sdk-go/commit/ec7847c184f0e85424c8e935e8db11a92498c421))

## [1.5.0](https://github.com/didit-protocol/sdk-go/compare/v1.4.0...v1.5.0) (2026-11-10)


### Features

* **spec:** sync API spec 2026-11-10 ([db7cac4](https://github.com/didit-protocol/sdk-go/commit/db7cac442d08dde73ca695760b5ca6672dcfea58))

## [1.4.0](https://github.com/didit-protocol/sdk-go/compare/v1.3.0...v1.4.0) (2026-11-09)


### Features

* **spec:** sync API spec 2026-11-09 ([2df4a58](https://github.com/didit-protocol/sdk-go/commit/2df4a5826f690b20a2edd487f79ed0c9083c92f9))

## [1.3.0](https://github.com/didit-protocol/sdk-go/compare/v1.2.0...v1.3.0) (2026-11-07)


### Features

* **spec:** sync API spec 2026-11-07 ([079b00d](https://github.com/didit-protocol/sdk-go/commit/079b00d734cf1e434c0d01f3bb76ce50d5e3d93b))

## [1.2.0](https://github.com/didit-protocol/sdk-go/compare/v1.1.0...v1.2.0) (2026-11-07)


### Features

* **spec:** sync API spec 2026-11-07 ([61aa72c](https://github.com/didit-protocol/sdk-go/commit/61aa72c50e15a5178dcce6a170c4901aa434458c))

## [1.1.0](https://github.com/didit-protocol/sdk-go/compare/v1.0.2...v1.1.0) (2026-11-05)


### Features

* **spec:** sync API spec 2026-11-05 (application data location) ([68684f9](https://github.com/didit-protocol/sdk-go/commit/68684f939c45af0a40496122fcff96a3d2e9fa38))

## [1.0.2](https://github.com/didit-protocol/sdk-go/compare/v1.0.1...v1.0.2) (2026-11-05)


### Bug Fixes

* **codegen:** import only the packages the generated code uses ([2e098d3](https://github.com/didit-protocol/sdk-go/commit/2e098d33b3721abd977cf901c30939c32159dc3b))
* **codegen:** keep multipart for identical JSON and multipart schemas with files ([0e7ca0a](https://github.com/didit-protocol/sdk-go/commit/0e7ca0a454628b391d104f2c788258a0eb4a4a08))
* **codegen:** keep the params type of identical JSON and multipart schemas ([0d3f8c9](https://github.com/didit-protocol/sdk-go/commit/0d3f8c910bcf8120bbf238a46420f46bd30518f2))
* **codegen:** multipart file lists send one file part per file ([867e1ce](https://github.com/didit-protocol/sdk-go/commit/867e1cef28dc80b02465769dbc073bd6f2604e41))
* **examples:** blockchain is optional in the wallet screening request ([bb8adb7](https://github.com/didit-protocol/sdk-go/commit/bb8adb72dc53d24677434507ce92e22343e3419d))

## [1.0.1](https://github.com/didit-protocol/sdk-go/compare/v1.0.0...v1.0.1) (2026-10-29)


### Miscellaneous Chores

* release 1.0.1 ([e0fb65b](https://github.com/didit-protocol/sdk-go/commit/e0fb65bfd03fc388db8e55d4d7daccec69de141b))

## [1.0.0](https://github.com/didit-protocol/sdk-go/compare/v0.1.0...v1.0.0) (2026-10-29)


### ⚠ BREAKING CHANGES

* **spec:** the API spec sync changes the SDK surface (nullable fields, removed fields, account.organizations.list returns a page, the session decision response is the SessionDecision component).

### Features

* add bank.account_check (POST /v3/bank/account-check/) and bank_verifications on the decision ([3dfa1b0](https://github.com/didit-protocol/sdk-go/commit/3dfa1b08fe624c00d4f9a20f3ff2e4d3b14d7d6c))
* **spec:** sync API spec 2026-10-23 ([#2](https://github.com/didit-protocol/sdk-go/issues/2)) ([eed196a](https://github.com/didit-protocol/sdk-go/commit/eed196a12a23a865ea09215448f326173c744f29))
* **spec:** sync API spec 2026-10-27 ([#7](https://github.com/didit-protocol/sdk-go/issues/7)) ([55fba87](https://github.com/didit-protocol/sdk-go/commit/55fba87cc8e17ba9cf438d0d975aca469187c480))


### Miscellaneous Chores

* release 1.0.0 ([53e2047](https://github.com/didit-protocol/sdk-go/commit/53e204768729614e83013dcf463901e73e9f31bf))
