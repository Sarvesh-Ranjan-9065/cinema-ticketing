# Secrets Directory

This directory stores sensitive configuration files (e.g. database passwords) as Docker secrets or local files mounted at runtime.

Rules:
- Never commit any secret file in this directory.
- Secrets are excluded by `.gitignore` except for this README.
- For local testing, generate secrets using `scripts/init-secrets.sh`.
