---
name: goal-deliver
description: Turn a change into a pull request on the item's branch, with work in progress pushed early, evidence for each acceptance criterion, the checks that ran and those that did not. Use for every change to this repository, by people and agents alike, from the first commit to marking the pull request ready.
---

# Deliver a change

One item, one branch, one small pull request (AGENTS.md, "Changes"). What a change must also touch, and the tasks it must pass, are in the [change map](../../../docs/engineering/change-map.md). The checks the pre-push hook and CI run are AGENTS.md's "Done means verified".

1. **Work on the item's branch**, `goal/<number>-<slug>`, made from the base your workspace note names: `main`, or an assembly's integration branch for a part of one. On a rework, or a run after one that failed, start from what is already pushed:
   ```sh
   git fetch origin && git log --oneline origin/main..HEAD
   ```
   Continue from the branch; do not start over. Once per clone, run `task hooks:install` so that `git push` runs `task check`.
2. **Push work in progress early.** A box or a laptop can die at any time, and only what is pushed survives. Commit and push after the first test that passes, and after every step that passes since (a layer, a criterion). Write messages in the repository's style: `area: what changed`, imperative, wrapped. An agent ends each message with the `Co-Authored-By:` line its run gives. The last commit before delivery says in its body what you ran to verify the change and what you could not run.
3. **Push only the item's branch:**
   ```sh
   git push -u origin HEAD
   ```
   Never push to `main` or to another branch, and never with `--force`. After you merge or rebase onto your base, run `task docs:gen` and commit `docs/engineering/change-map.md` and `docs/adr/README.md` when they changed. Resolve a conflict on either of those two files by running `task docs:gen`, never by editing it by hand. Never push with `--no-verify` unless the pull request says that you did and why.
4. **Open a draft pull request on the first push:**
   ```sh
   gh pr create --draft --base <the base your workspace note names> --title "<the item's title>" --body-file <file>
   ```
   After that, keep the body current with `gh pr edit <number> --body-file <file>`. The body holds:
   - every acceptance criterion, whether it is met and its evidence: the test, the command and its result, or the file and line that shows it;
   - each check you ran, with its result;
   - what you did not run, and why (no PostgreSQL, no browser, no credentials), so a reviewer knows what CI or a person still has to cover;
   - what a person must do after the merge (an apply, a deploy, an edit outside the repository), in order;
   - follow-ups you found and left out of scope.
5. **Verify after the last push.** Run the item's verification and the checks of what you touched (the change map's Run column). Fix what fails, then push again.
6. **Mark it ready** with `gh pr ready` once that verification passes. GitHub merges no draft, and the reviewer's approval merges a ready pull request.
7. **Leave the gate alone.** Never merge, close, approve or delete a pull request or a branch yourself; the review gate merges.
