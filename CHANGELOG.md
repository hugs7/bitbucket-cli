# Changelog

## [0.7.0](https://github.com/hugs7/bitbucket-cli/compare/v0.6.0...v0.7.0) (2026-05-14)


### Features

* reviewers visible section pr view screen ([17587a6](https://github.com/hugs7/bitbucket-cli/commit/17587a6393ea0457ef743d8615232e71871ba5ad))

## [0.6.0](https://github.com/hugs7/bitbucket-cli/compare/v0.5.0...v0.6.0) (2026-05-06)


### Features

* auto-clear toasts and add :messages history ([3ac6901](https://github.com/hugs7/bitbucket-cli/commit/3ac69014a5f90c90815b6c69d11249b901ec8a28))
* auto-clear toasts and add :messages history ([f995a88](https://github.com/hugs7/bitbucket-cli/commit/f995a88f4658d089d0f396b79254013820728097))
* copy pr link hotkey ([1078d27](https://github.com/hugs7/bitbucket-cli/commit/1078d273b5685ad2672aeb8906355d8eb5953e9e))
* default PR name ([7846203](https://github.com/hugs7/bitbucket-cli/commit/7846203c1c848c3ceb5e490b0c1c8acee461f9c9))
* diff search and custom keybindings ([7677699](https://github.com/hugs7/bitbucket-cli/commit/76776994aefc87326fe2622d1741299bb0476ffb))
* disappearing toast ([6a0fc55](https://github.com/hugs7/bitbucket-cli/commit/6a0fc554ae7f215cd3c4fa849804f0820b95f09c))
* edit PR target branch ([dfe78af](https://github.com/hugs7/bitbucket-cli/commit/dfe78af37a3c80102587ffa06871156b3508c798))
* sync source branch to remote prompt ([eec63e2](https://github.com/hugs7/bitbucket-cli/commit/eec63e269c4a0961f0229abb6eda88948dab0365))


### Bug Fixes

* history back stack pr ([ffa02df](https://github.com/hugs7/bitbucket-cli/commit/ffa02dfa0bd615bcac481e597cbcb4abafe5096c))
* PR title as hint ([8f1cfaa](https://github.com/hugs7/bitbucket-cli/commit/8f1cfaa4b9e00d02c32b2a528830d96c03f2964d))
* reevaluate PR title placeholder on source change ([c302367](https://github.com/hugs7/bitbucket-cli/commit/c302367e3077c4a8dae48a3f740bf54b1eee05c7))

## [0.5.0](https://github.com/hugs7/bitbucket-cli/compare/v0.4.0...v0.5.0) (2026-04-27)


### Features

* **cli:** support bb &lt;path&gt; and bb pr &lt;url&gt; as direct entry points ([52bc8a3](https://github.com/hugs7/bitbucket-cli/commit/52bc8a30e1f2535eaf628944f29c9ae40009b118))


### Bug Fixes

* **pr edit:** preserve reviewers when updating description ([cb45a8e](https://github.com/hugs7/bitbucket-cli/commit/cb45a8e58ad93e6d93cbba5be1963dd28d699ed0))

## [0.4.0](https://github.com/hugs7/bitbucket-cli/compare/v0.3.0...v0.4.0) (2026-04-27)


### Features

* **auth:** reject http(s):// in host input ([599229c](https://github.com/hugs7/bitbucket-cli/commit/599229c4bb32c1386056eda1bee6d1ace88ea250))
* **home:** T toggles pane focus so the README preview can scroll ([6216f31](https://github.com/hugs7/bitbucket-cli/commit/6216f31a2d20dd930b901dbe0f50c4cf60fc458e))
* **pr create:** rebind tab to accept autocomplete suggestion ([a980d5f](https://github.com/hugs7/bitbucket-cli/commit/a980d5fff52d403fb7d0a8f32413a8be2d52fdfe))
* **pr:** autocomplete-driven PR create form (replaces vim template) ([4248915](https://github.com/hugs7/bitbucket-cli/commit/4248915a27dc6c18025f7534d83b86200ffbc177))
* **pr:** debounced reviewer directory search ([3e49b91](https://github.com/hugs7/bitbucket-cli/commit/3e49b910de2e94a1aa42ab8ed00e25f298994308))
* **pr:** delete-PR action, full-help footer, alt+enter submit ([0286eca](https://github.com/hugs7/bitbucket-cli/commit/0286eca914a629847fa009200ec2f7c84177b9c0))
* **pr:** incremental picks + common-reviewers history in reviewer search ([0c618c3](https://github.com/hugs7/bitbucket-cli/commit/0c618c3068c53122ad24d2549cd4236a497bd744))
* **pr:** keep existing reviewers visible while searching ([7a15777](https://github.com/hugs7/bitbucket-cli/commit/7a15777e6c154f5ce32dd674371c23611ecf82ee))
* **pr:** pick merge strategy in the merge-confirm dialog ([ec7c7e9](https://github.com/hugs7/bitbucket-cli/commit/ec7c7e9c4ebd47abd9a89afb00e2ab1ceceac40d))
* **pr:** swap reviewer-search keys — enter submits, tab queues ([a480299](https://github.com/hugs7/bitbucket-cli/commit/a4802994a5d852ac328e6fd66cec78f1f0385db5))
* **pr:** unified manage-reviewers modal with stacked backdrop ([2522991](https://github.com/hugs7/bitbucket-cli/commit/252299134474a07a2a9f6aee4c8b3f5ca53977df))
* **pr:** visible Actions panel + reviewer status badge in detail pane ([c8e141a](https://github.com/hugs7/bitbucket-cli/commit/c8e141aa68009a79c1728e18e77d2faa6523155b))
* **release:** ship shell completions in every install path ([9102684](https://github.com/hugs7/bitbucket-cli/commit/910268418416ed8b0f48cc0efb17da0758a6a51b))
* support tasks on PRs ([5ffa2c3](https://github.com/hugs7/bitbucket-cli/commit/5ffa2c338034e43875b1065c4bc4bfacd669b204))
* **tui:** render PR descriptions as markdown via shared mdrender ([3c95eab](https://github.com/hugs7/bitbucket-cli/commit/3c95eab803820b0723f680a52ff180702f30d913))
* **upgrade:** --insecure / --no-proxy flags for corp networks ([d82a32b](https://github.com/hugs7/bitbucket-cli/commit/d82a32b1d13ffbcd1f6a061db79cc66a6e099f3c))
* **upgrade:** notify the user when a newer release is available ([4304800](https://github.com/hugs7/bitbucket-cli/commit/430480038b94c2082f18d7474ce5f40d7d27f9d9))
* **upgrade:** refuse to clobber package-manager-owned binaries ([2e8d30c](https://github.com/hugs7/bitbucket-cli/commit/2e8d30ccffa27b7fff6e8b7583463c66a0d7910d))


### Bug Fixes

* **home:** Enter on a dashboard repo row opens the repo overview ([75dbbd8](https://github.com/hugs7/bitbucket-cli/commit/75dbbd8584246cdf092bfa1709d432131627e776))
* **home:** Enter on Favourites / Browse repos opens the repo overview ([7a61d3f](https://github.com/hugs7/bitbucket-cli/commit/7a61d3f727c5867619a9d514f1fd853eabf74d74))
* **home:** forward mouse wheel events to the dashboard viewport ([6ea5c92](https://github.com/hugs7/bitbucket-cli/commit/6ea5c9242497c3b804c4d38fbb5c46f756d2961b))
* **home:** populate dashVP content in Update so scroll actually moves ([9f6f985](https://github.com/hugs7/bitbucket-cli/commit/9f6f985e26a2eb65d8e9cefc8171bca32f23d72e))
* **pr:** require explicit selection before Enter submits picks ([56d0a81](https://github.com/hugs7/bitbucket-cli/commit/56d0a8114509f63a84861669401bd574cef19d36))
* **tui:** force dark glamour style so README markdown actually styles ([0f8191c](https://github.com/hugs7/bitbucket-cli/commit/0f8191c22915f78a3c6ae87c0cba7995c45edd74))

## [0.3.0](https://github.com/hugs7/bitbucket-cli/compare/v0.2.0...v0.3.0) (2026-04-27)


### Features

* **packaging:** rename Homebrew formula to bitbucket-cli, keep bb binary ([8131618](https://github.com/hugs7/bitbucket-cli/commit/81316186ebfbebdce5d540ca6948653688250c18))

## [0.2.0](https://github.com/hugs7/bitbucket-cli/compare/v0.1.0...v0.2.0) (2026-04-27)


### Features

* **packaging:** rename Linux package to bitbucket-cli, keep bb binary ([2f7cb74](https://github.com/hugs7/bitbucket-cli/commit/2f7cb744b2515303d0de4079cd9f5a7cc2d7f673))


### Bug Fixes

* **release:** point Cloudsmith publishers to renamed hugs7/bitbucket-cli repo ([2c2a225](https://github.com/hugs7/bitbucket-cli/commit/2c2a225457460d17924f13937dc7f4ce731d889c))
