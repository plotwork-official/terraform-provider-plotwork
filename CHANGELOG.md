## 0.1.0 (unreleased)

FEATURES:

* **New data source:** `plotwork_import` turns Terraform, plan JSON, Bicep, Kubernetes, Compose, SQL and the other formats Plotwork reads into diagram source.
* **New data source:** `plotwork_render` renders diagram source to SVG, for any storage resource to write.
* **New resource:** `plotwork_diagram` publishes to a personal or an organisation's library, replaces and cleans up on change, and keeps a stable link with `alias`.
* Release channels: a pre-release version (`0.1.0-beta.1`) talks to the preview API by default, a stable one to production.
