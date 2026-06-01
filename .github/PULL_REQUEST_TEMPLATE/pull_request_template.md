## Linked issue

Closes #

> Every PR must close an issue. If one does not exist yet, create it before opening this PR.

---

## Why

What problem does this PR solve? Paste the issue context here if it is not obvious from the title.

---

## What changed

List the concrete changes made in this PR. Be specific — not "improved chunking" but "added markdown section splitter that splits on H1/H2 headings".

-

---

## Type of change

- [ ] Bug fix
- [ ] New feature
- [ ] Refactor (no behaviour change)
- [ ] Documentation
- [ ] Tests
- [ ] Chore (deps, build, tooling)

---

## How to test

Exact steps to verify this works locally. Another developer should be able to follow these without asking you anything.

```bash
# example
docker compose up -d
curl http://localhost:8080/health
```

---

## Screenshot or recording

_If this PR touches the UI, attach a screenshot or screen recording. Delete this section if not applicable._

---

## Checklist

- [ ] My branch is up to date with `main`
- [ ] I tested this locally following the steps above
- [ ] Tests pass — `go test ./...` / `npm run test`
- [ ] Linting passes — `golangci-lint run` / `npm run lint`
- [ ] I updated docs if behaviour changed
- [ ] I wrote or updated an ADR if this involves a significant technical decision