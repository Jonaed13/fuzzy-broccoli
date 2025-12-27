# AFNEX Bot - AI Assistant Guidelines

> Optimized rules for Solana trading bot development.

---

## 1. CORE BEHAVIORAL PROTOCOLS

### Default Mode
- **Execute immediately.** No philosophical lectures.
- **Concise answers.** Output code first, explanations second.
- **Follow instructions.** Do not deviate.

### ULTRATHINK Protocol (Trigger: "ULTRATHINK")
- **Maximum Depth:** Exhaustive, multi-dimensional reasoning.
- **Analysis Lenses:** Technical (performance, state), UX (cognitive load), Accessibility, Scalability.
- **Prohibition:** Never use surface-level logic.

---

## 2. PROJECT AWARENESS

### Required Files
| File | Purpose |
|------|---------|
| `PLANNING.md` | Architecture, goals, constraints |
| `TASK.md` | Current tasks, progress tracking |
| `config.yaml` | Runtime configuration |

### Before Every Task
1. Check `TASK.md` - add new tasks with date if missing.
2. Mark completed tasks immediately.
3. Add discoveries under "Discovered During Work".

---

## 3. CODE STRUCTURE (Go)

### File Limits
- **Max 500 lines per file.** Refactor if approaching.

### Module Organization
```
internal/
├── blockchain/   # RPC, transactions
├── trading/      # Executor, positions
├── tui/          # UI components
├── signal/       # Telegram parsing
└── storage/      # SQLite persistence
```

### Naming
- **Files:** `lowercase_snake.go`
- **Functions:** `camelCase`
- **Structs:** `PascalCase`

---

## 4. TUI DESIGN PHILOSOPHY

### Visual Principles
- **Intentional Minimalism:** Every element must have purpose.
- **Anti-Generic:** Reject template looks. Bespoke layouts only.
- **Perfect Spacing:** Whitespace is a feature, not emptiness.

### Current Theme: Tokyo Night
- Background: `#1a1b26`
- Border: `#7aa2f7`
- Gauge: `#bb9af7` (purple)
- Sparkline: `#9ece6a` (green)

### UI Modes
| Mode | Keys 1/2/3 | Footer Style |
|------|-----------|--------------|
| Classic (1) | Full views | Text hotkeys |
| Crossterm (2) | Actions | Button bar |

---

## 5. TESTING & RELIABILITY

### Requirements
- `go vet ./...` must pass.
- `go build` must succeed.
- Race detector: `go build -race` should compile.

### Before Commit
1. Run `go vet ./...`
2. Build: `go build -o bin/afnex-bot ./cmd/bot`
3. Test UI modes: 1 (Classic) and 2 (Crossterm)

---

## 6. RESPONSE FORMAT

### Normal Mode
1. **Rationale** (1 sentence).
2. **The Code.**

### ULTRATHINK Mode
1. **Deep Reasoning Chain.**
2. **Edge Case Analysis.**
3. **The Code** (production-ready).

---

## 7. PROHIBITIONS

- ❌ Never hallucinate libraries.
- ❌ Never delete code without instruction.
- ❌ Never assume missing context - ask.
- ❌ Never ignore `TASK.md`.
- ❌ Never create files > 500 lines.
