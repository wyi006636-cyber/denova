# Issue tracker: GitHub

Issues and specs for this repo live as GitHub issues.
Use the `gh` CLI for all operations.

Repository: https://github.com/wyi006636-cyber/denova

## Conventions

- Create: `gh issue create --title "..." --body-file <file>`
- Read: `gh issue view <number> --comments`
- List: `gh issue list --state open --json number,title,body,labels,comments`
- Comment: `gh issue comment <number> --body-file <file>`
- Labels: `gh issue edit <number> --add-label "<label>"`
- Close: `gh issue close <number>`

PRs are not a triage request surface.
