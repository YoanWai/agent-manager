# Governance

[@YoanWai](https://github.com/YoanWai) is the maintainer of agent-manager. Roles, merges, the roadmap, and how decisions are recorded are described below.

## 1. Roles

| Role | Who | What they do |
| --- | --- | --- |
| Maintainer | [@YoanWai](https://github.com/YoanWai) | Reviews and merges, runs the roadmap, cuts releases, and holds the reserved powers in section 3. |
| Collaborator | [@wainmann/collaborators](https://github.com/orgs/wainmann/teams/collaborators) (Write access) | Reviews and merges pull requests, edits the roadmap board, and helps newcomers. Current members are listed in [MAINTAINERS.md](MAINTAINERS.md). |
| Triager | [@wainmann/triage](https://github.com/orgs/wainmann/teams/triage) | Labels, closes, reopens, and assigns issues and pull requests. No push access and no roadmap edits. |
| Contributor | Anyone | Opens issues, Discussions, and pull requests. |

## 2. Merging

Any collaborator can merge a pull request once another collaborator or the maintainer has approved it. Nobody approves their own pull request.

Code-owner review is enforced by [CODEOWNERS](.github/CODEOWNERS) and branch protection (one approval, a code-owner review, and approval of the last push).

## 3. Reserved for the maintainer

Only the maintainer:

- Cuts releases and tags
- Changes repo settings, rulesets, and access
- Handles security fixes and advisories
- Adds or removes collaborators
- Holds a veto on breaking changes and project direction

## 4. Roadmap

The maintainer and collaborators run the public roadmap board [agent-manager roadmap](https://github.com/orgs/wainmann/projects/1) together, across every column.

New themes or big changes start as a Discussion in the [Ideas](https://github.com/wainmann/agent-manager/discussions/categories/ideas) category.

## 5. Triagers and becoming a collaborator

Triagers only label, close, reopen, and assign issues and pull requests. They do not edit the roadmap and they do not push.

A collaborator can nominate a triager for promotion, or a triager can ask once they have proven themselves. The maintainer decides, and can also promote someone directly.

## 6. Inactivity

After six months with no activity, a collaborator moves to emeritus in [MAINTAINERS.md](MAINTAINERS.md). An emeritus collaborator can return anytime.

## 7. Write decisions down

Talk anywhere (in chat, on Discord, or on a call). A decision made there is written up on the GitHub issue or Discussion it belongs to within a day.
