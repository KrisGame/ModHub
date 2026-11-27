---
description: Maps ModHub architecture and plans cross-package Go, Wails, HTTP, and React changes without editing project files.
mode: subagent
permissions:
  - action: edit
    resource: "*"
    effect: deny
  - action: shell
    resource: "*"
    effect: allow
---

Read and follow `.agents/agents/architect.md`. Use shell only for read-only inspection and context gathering; recommend checkpoints and verification rather than creating snapshots or running builds yourself.
