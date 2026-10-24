## 0.2.0 (unreleased)

FEATURES:

* **New resource:** `plotwork_organization_brand` sets an organisation's brand colours. Colours are compared as lower-case `#rrggbb`, so case and the short form are no change; `effective_secondary` and `effective_accent` give the colours Plotwork makes from the primary.
* **New resource:** `plotwork_organization_icon` uploads a custom icon from `svg` text (`file()`, an S3 object, a `data "http"` download) or an https `source_url` the provider downloads during plan. `content_sha256` follows the file, so a new file, here or at the address, uploads it again. Imports as `<organisation>/<name>`.
* **New resource:** `plotwork_pattern` saves an approved pattern from a pattern file. The file is compared as JSON, so spacing and key order are no change; a new `name` in it replaces the pattern, and a pattern changed in the portal is put back by the next apply. Imports as `<organisation>/<name>`.
* The design resources need the owner or an admin, or an organisation key with the new `design:write` scope; a 403 says so.

## 0.1.0 (unreleased)

FEATURES:

* **New data source:** `plotwork_import` turns Terraform, plan JSON, Bicep, Kubernetes, Compose, SQL and the other formats Plotwork reads into diagram source.
* **New data source:** `plotwork_render` renders diagram source to SVG, for any storage resource to write.
* **New resource:** `plotwork_diagram` publishes to a personal or an organisation's library, replaces and cleans up on change, and keeps a stable link with `alias`.
* Release channels: a pre-release version (`0.1.0-beta.1`) talks to the preview API by default, a stable one to production.
