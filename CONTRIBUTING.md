# Contributing

[中文版本](CONTRIBUTING.zh-CN.md)

Thanks for your interest in contributing to Go Proxy Server.

## Development Workflow

1. Fork the repository and create a feature branch.
2. Make focused changes with clear commit messages.
3. Run validation locally:
   - `go test ./...`
   - `cd web-ui && npm run build` when frontend files change
4. Update documentation when behavior, commands, or APIs change.
5. Open a pull request describing:
   - what changed
   - why it changed
   - how it was tested

## Contribution Guidelines

- Keep changes scoped; avoid unrelated refactors in the same PR.
- Preserve existing platform behavior unless the change explicitly updates it.
- Prefer clear, maintainable code over clever shortcuts.
- When adding new commands or APIs, update both English and Chinese docs.

## Reporting Bugs

Please include as much context as possible:

- operating system and version
- command used to start the service
- relevant configuration
- logs or screenshots
- steps to reproduce

## Security Issues

Please do not open a public issue for vulnerabilities first. See [SECURITY.md](SECURITY.md).
