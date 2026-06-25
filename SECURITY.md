# Security Policy

## Supported Versions

Security fixes are provided for the latest released version and the current `main` development branch.

## Reporting a Vulnerability

Please do not report security vulnerabilities in public issues.

Use GitHub's private vulnerability reporting for this repository if it is available. If private reporting is not available, open a minimal public issue asking for a private contact path and do not include exploit details, logs, tokens, database credentials, or sensitive environment information.

When reporting, please include:

- affected version or commit
- a concise description of the issue
- reproduction steps or proof of concept
- expected impact
- any known mitigations

## Response

This is a small open-source project without a formal security response SLA. Maintainers will make a best effort to acknowledge valid reports, investigate them, and publish a fix or mitigation when appropriate.

## Scope

`uptime` is a lightweight `net/http` middleware and status dashboard.

Security reports are most useful when they affect:

- exposure of sensitive runtime or storage data
- unsafe HTTP behavior in the dashboard or JSON API
- request handling bugs that can affect the wrapped service
- SQLite or PostgreSQL store correctness under concurrent use
- alert hook de-duplication failures that can cause notification storms
- dependency vulnerabilities with a practical impact on this package

Out of scope for this package:

- dashboard authentication or authorization policy
- reverse proxy configuration
- network firewall rules
- notification provider account security
