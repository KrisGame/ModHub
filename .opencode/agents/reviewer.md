---
description: Independently reviews ModHub changes for regressions, contract drift, and security issues.
mode: subagent
permissions:
  - action: edit
    resource: "*"
    effect: deny
  - action: shell
    resource: "*"
    effect: allow
---

Read and follow `.agents/agents/reviewer.md`. Use shell for inspection and verification only; do not edit project files or commit changes.
