# TODO

Items tracked here represent ideas discussed but not yet implemented,
known improvements, and planned work. This file is maintained manually
and referenced during planning sessions.

---

## Planned for v1.0.0

### Refactor

- [ ] Review and refactor code written without TDD in the early stages of the project
- [ ] Replace fragile pending worker and pending SSH session attribution
- [ ] Remove stdout-capture-driven command execution from shipped TUI flows
- [ ] Consolidate module execution into one TUI-first runtime path
- [ ] Split `internal/session.go` by responsibility after seam cleanup
- [ ] Split `internal/tui/app.go` by responsibility after seam cleanup
- [ ] Dead code and redundancy audit
- [ ] Standards and consistency check across the codebase

### Feature

- [ ] Write missing tests for untested packages and functions, guided by `coverage.out`

## Ideas and future work

### Post-1.0

- [ ] Add AMSI bypass
- [ ] Add SpawnAs via RunasCs.exe
- [ ] Consider adding keylogger and screenshot capabilities
