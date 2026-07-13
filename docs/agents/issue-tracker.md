# Issue tracker: GitHub

Issues and PRDs for this repository live as GitHub issues. Use the `gh` CLI
for all operations and let it infer the repository from `git remote -v`.

## Conventions

- Create: `gh issue create --title "..." --body "..."`.
- Read: `gh issue view <number> --comments` and fetch labels with the issue.
- List: `gh issue list --state open --json number,title,body,labels,comments`.
- Comment: `gh issue comment <number> --body "..."`.
- Label: `gh issue edit <number> --add-label "..."` or `--remove-label "..."`.
- Close: `gh issue close <number> --comment "..."`.

Use a heredoc or body file for multiline issue bodies rather than embedding a
large body in a shell argument.

## Pull requests as a triage surface

**PRs as a request surface: no.** Set this to `yes` only if external pull
requests should enter the same triage queue as issues.

GitHub shares one number space across issues and pull requests. Resolve an
ambiguous `#<number>` with `gh pr view <number>` and then fall back to
`gh issue view <number>`.

## Skill operations

- "Publish to the issue tracker" means create a GitHub issue.
- "Fetch the relevant ticket" means run `gh issue view <number> --comments`.

## Wayfinding operations

- The map is one issue labelled `wayfinder:map`.
- Tickets are GitHub sub-issues where supported. Otherwise link them from a
  task list in the map and add `Part of #<map>` to each ticket.
- Use `wayfinder:<type>` labels for `research`, `prototype`, `grilling`, and
  `task` tickets.
- Represent blocking with GitHub native issue dependencies. If unavailable,
  add a `Blocked by: #<number>` line to the child issue.
- Claim work with `gh issue edit <number> --add-assignee @me`.
- Resolve work by commenting with the result, closing the child issue, and
  recording the decision pointer in the map.
