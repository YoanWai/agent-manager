---
name: agent-manager-reference
description: Query agent-manager.dev's reference API and MCP server for documentation, the coding CLIs it manages, the tools its MCP server exposes, and the current release. Use instead of scraping the website's HTML.
license: Apache-2.0
homepage: https://agent-manager.dev/api
repository: https://github.com/YoanWai/agent-manager
---

<!--
Why this skill exists: it gives an AI agent the exact endpoints, parameters and MCP tool
names for answering questions about agent-manager, so it queries structured data instead
of guessing URLs or scraping HTML. The site serves this file, digest-sealed, at
https://agent-manager.dev/skills/agent-manager-reference/SKILL.md; the copy in this
repository is what makes it installable through skills.sh.
-->

# Query the agent-manager reference API

agent-manager.dev publishes its own reference data as JSON, as Markdown, and over MCP. Reach
for these instead of parsing the marketing HTML.

## When to use this

- Answering "does agent-manager support X", "what does it cost", "how do I install it".
- Pulling a documentation page as clean Markdown for context.
- Listing the MCP tools the binary exposes before telling a developer what an agent can do.

## Markdown from any page

Every page serves Markdown from its own URL, and from the `.md` path beside it:

```bash
curl -H "Accept: text/markdown" https://agent-manager.dev/docs/install/
curl https://agent-manager.dev/docs/install.md
```

## The JSON API

No key, no account, no rate limit to negotiate. Read-only.

| Endpoint | Returns |
| --- | --- |
| `GET /api` | Every endpoint, with its operationId |
| `GET /api/docs` | Every page: slug, title, description, URL, Markdown URL |
| `GET /api/docs/{slug}` | One page, with its full Markdown body |
| `GET /api/search?q=` | Search over every page, with the matching line |
| `GET /api/agents` | The coding CLIs with built-in status detection |
| `GET /api/mcp/tools` | The tools the agent-manager binary exposes |
| `GET /api/release` | The newest release and its install commands |
| `POST /api/batch` | Several of the above in one round trip |

The OpenAPI description is at https://agent-manager.dev/openapi.json. Errors are JSON with a
stable `error.code`, a `message` and a `hint`.

## Over MCP

Streamable HTTP, no authentication:

```
https://agent-manager.dev/mcp
```

Tools: `search_docs`, `get_doc_page`, `list_doc_pages`, `list_supported_agents`,
`list_managed_mcp_tools`, `get_latest_release`. Every page is also an MCP resource. Read
https://agent-manager.dev/mcp/server-card before connecting.

## Natural language

```bash
curl -X POST https://agent-manager.dev/ask -H 'content-type: application/json' \
  -d '{"query":"how do I review an agent diff"}'
```
