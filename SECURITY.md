# Security

Report a vulnerability privately with GitHub's **Report a vulnerability** button on this repository's Security tab. Please do not open a public issue.

We answer within three working days and tell you when a fix ships.

## What the provider sends

| Data source or resource | Sends to the Plotwork API |
|---|---|
| `plotwork_import` | The text of the files you pass it |
| `plotwork_render` | Diagram source |
| `plotwork_diagram` | Diagram source; the API stores the rendered SVG |

The API key travels in the `X-API-Key` header and never to the CDN that serves public diagrams. Pass configuration, not state: Terraform state can hold secrets, and the provider cannot tell which values are secret.
