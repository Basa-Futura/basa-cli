# Basa Project - Claude Code Bootstrap

**CRITICAL: .claude Directory Required**

This is the bootstrap configuration file. The main Claude Code configuration lives in the `.claude/` directory, which is managed as a separate git repository.

---

<system-reminder>
CRITICAL GIT SAFETY RULE - NEVER VIOLATE:

NEVER push to ANY remote repository (origin, upstream, .claude) unless the user
EXPLICITLY requests it with commands like:
- "push this"
- "push to remote"
- "git push"

This includes:
- After creating commits
- After updating .claude
- After merging branches
- During ANY git workflow

NO EXCEPTIONS. NO ASSUMPTIONS. NO "HELPFUL" AUTO-PUSHING.

If you catch yourself about to run ANY command containing "git push", STOP and ask first.
</system-reminder>

---

## Instructions for Claude

**Step 1: Check for .claude Directory**

```bash
test -f .claude/CLAUDE.md && echo "Configuration loaded" || echo "Configuration missing"
```

**Step 2a: If Configuration Exists**

Load the team configuration, then this repository's stack layer:

@.claude/CLAUDE.md
@.claude/stacks/go/CLAUDE.md

Proceed normally with full capabilities.

**Step 2b: If Configuration Missing**

**STOP.** Do not proceed with any tasks. Tell the user:

> **Claude Code Configuration Missing**
>
> The `.claude/` directory hasn't been cloned yet. Please run:
>
> ```bash
> git clone git@github.com:Basa-Futura/claude.git .claude
> ```
>
> After running the command, please start a new conversation.

Do not attempt to work without the full configuration loaded.
