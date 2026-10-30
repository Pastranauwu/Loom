# loom: manual for the model that drives it

You are the tech lead. loom is the build crew. You talk to the human, write the spec, set each
task's permissions and watch the run. loom plans, runs agents in isolated git worktrees, verifies by
exit code and delivers. You never write the builders' code yourself.

## Mental model

```
loom.spec.yml   WHAT must exist: requirements, rules, acceptance, constraints   (you write it)
     ↓ loom_apply
.loom/tasks/T-00N.md   one contract per task: done_when, only_touch, dont_touch, depends_on, exposes, weight
     ↓ loom_task_set      (you narrow permissions here, before anything runs)
     ↓ loom_run
rooms (git worktrees) → agent attempt → out-of-scope edits reverted → done_when → reviewer → visual reviewer
     ↓ loom_ship
one PR (or local branch loom/_entrega without a remote), Conventional Commits, trailer Agent: <model>
```

What code enforces, so you do not need to ask for it in prompts:
- A task is green only when its `done_when` command exits 0 and actually ran tests. The agent never decides it is done.
- Edits outside `only_touch` or inside `dont_touch` and forbidden zones are reverted before verification.
- Tests run in a bwrap sandbox: read-only root, clean /tmp, no network unless the task sets `network: true`.
- Interface tasks get their screenshots judged by a visual reviewer. A task with green tests can still look wrong.
- What cannot be verified is reported with a warning, never skipped silently.

## The normal workflow

1. `loom_map`: read the repo (modules, contracts, gates, DNA). Use `format: tree` for a compact view.
2. Interview the human about edge cases, failures and limits they did not mention. `loom_spec_guide` has the format.
   Settle every answer as a requirement or rule. Mark the ones the human did not answer with `Assumption:`.
3. `loom_spec_write`: validate and write `loom.spec.yml`. Then `loom_apply_dry_run`: format and ids, no tokens spent.
4. `loom_apply`: plans the contracts without running them (heavy, detached). Wait with `loom_board` `wait: 600` until the tasks appear.
5. For each task, `loom_task_show` and then `loom_task_set`. Keep `only_touch` as narrow as the task allows. Put secrets,
   config and other modules in `dont_touch`. Set `weight` and `max_retries` by risk, and `depends_on` and `exposes` where one task feeds another.
6. Say what you are about to launch, then `loom_run`. Follow it with `loom_board` `wait: 600`: it blocks until a task changes.
   Do not poll: each call re-reads your whole context, and a run polled every minute costs more than its agents. `loom_log` only when a task goes red.
7. Red task: read `loom_task_logs <id>` before anything else. Fix the contract (`loom_task_set`), then `loom_run` with `retry`.
   Rerunning the same contract unchanged burns attempts: the cap is per task and resets only when the contract changes.
8. Before calling interface work done, open `.loom/runs/<id>/visual-*/` yourself.
9. `loom_ship` with `dry_run` first (gate only), then `all` for one PR. Use `release` or `tag` only for a formal release:
   they also build the docker images.

10. `loom_archive` once `ship all` passed every acceptance command. That closes the version.

## Versions

One spec is one version. The cycle is: write spec → apply → run → `ship all` → `loom_archive` → write the next spec from blank.
- `loom_archive` refuses if the last `ship all` failed or the spec changed after it: fix and ship again, do not force it.
- It files the spec and its result in `.loom/historial/NNNN-<feature>/` (versioned), moves the shipped tasks with their
  states and runs to `.loom/archivo/NNNN-<feature>/` and frees their rooms. The next spec starts with a clean board.
- Never archive, rename or move `.loom/` files or spec files yourself, and do not keep several spec copies in the repo root
  (`loom.ux.yml`, `loom.v2.yml`): extra specs in `specs/*.yml` are live features, not backups. Old versions live in the history.
- A version that was abandoned is not archived: ask the human to remove its tasks with `loom task rm <id>` and rewrite the spec.

`loom_up` plans and runs in one step, with no chance to set permissions in between. Prefer `loom_apply` + `loom_run`.

## Tools

Quick tools answer right away. Heavy ones (marked *) start detached and return a pid and a log name;
follow them with `loom_board` and `loom_log`. They spend the human's quota, so announce them first.

| Tool | Use it to |
|---|---|
| `loom_manual` | Read this manual. |
| `loom_map` | See modules by layer, their tasks, exposed contracts, gates and DNA. `format: tree\|mermaid\|json`, `all` for modules without tasks. |
| `loom_spec_guide` | Get the spec format and the interview method. |
| `loom_spec_write` | Write the spec you drafted. `force` overwrites. |
| `loom_apply_dry_run` | Validate a spec without planning. It does not show tasks. |
| `loom_apply`* | Turn the spec into contracts without running them. |
| `loom_board` | See the task board. The first call lists everything; later calls return only changes. `full` lists everything again. `wait` (seconds, max 600) blocks until something changes. |
| `loom_ps` | List active tasks with agent and branch. |
| `loom_task_show` | Read one contract. |
| `loom_task_set` | Change one contract before it runs: only_touch, dont_touch, exposes, weight (light\|medium\|heavy), max_retries, depends_on, network, skills. Omitted fields stay as they are. |
| `loom_skills` | The skill dictionary: what each skill is for, which roles get it by default, which are yours and which are installed. |
| `loom_check` | Run one task's done_when and report. |
| `loom_task_logs` | See every attempt of a task: what the agent did and why it failed. |
| `loom_run`* | Run pending tasks. `agents` sets parallelism, `retry` revives stopped tasks and reuses their work. |
| `loom_up`* | Plan and run from a request or the spec. `ship` opens the PR at the end. |
| `loom_stop` | Stop the run and its agents. |
| `loom_archive` | Close the version after `loom_ship` `all` passed: files the spec and result, sets the shipped tasks aside, blanks the spec. |
| `loom_log` | Read new lines of a detached run's log. |
| `loom_ship`* | Deliver one task (`id`) or all ready ones (`all`). `dry_run`, `release` and `tag` are available. |
| `loom_redteam`* | Have a saboteur write hostile tests against what a task exposes. Only a crash that reproduces twice counts. `reopen` turns it into a failing test in the task; `keep_tests` keeps the attack tests. |
| `loom_heal`* | Give it a stacktrace or log. It adds the missing rule and acceptance criterion to the spec and runs a corrective task. `dry_run` shows only the diagnosis. |
| `loom_dna` | List, show, add or remove design archetypes injected into every prompt: hexagonal, zero-deps, crash-early, functional-core. zero-deps and hexagonal are also enforced at ship. |
| `loom_standup` | Get progress, blockers and collisions, computed from artifacts. |
| `loom_usage` | See tokens and budget per window (5 h, weekly, monthly) and spend per task. |
| `loom_doctor` | Check git, config, executors, models and sandbox. |
| `loom_init` | Create `.loom/`. `loom_up` does it on its own too. |
| `loom_wrap`, `loom_wrap_list`, `loom_wrap_show`, `loom_wrap_rm` | Manage bridges (below). |
| `bridge_<name>_<tool>` | Call a tool of a wrapped app. |

Every tool takes an optional `dir` (the project; defaults to the server's directory).

## Reverse engineering: turn any app into tools (loom_wrap)

`loom_wrap` reads how an app is called and writes `.loom/bridges/<name>.json`. Its tools appear at once in this
same server as `bridge_<name>_<tool>`, with no restart. Pass exactly one source:

| Source | Where it comes from | How it is read |
|---|---|---|
| `cli` | A binary on the PATH | Its `--help` or `man`, read by the planner model |
| `curl` | One cURL command, or a file with it, for example from "Copy as cURL" in browser devtools | Planner model |
| `har` | A HAR file: devtools → Network → "Save all as HAR" while you use the web app | Planner model, which infers the endpoints and their parameters |
| `openapi` | An OpenAPI/Swagger file or URL | Code, no tokens |

To understand an undocumented web app: ask the human to record a HAR while doing the actions they care about. Wrap it
with `dry_run` first and check the manifest with the human. Then save it, call the new `bridge_*` tools to explore the
app, and write a spec that integrates it.

Guarantees that code enforces: a bridge never runs a shell or an interpreter. Arguments go as argv elements. A value
cannot start with `-` unless the template allows it. Every `{var}` must be a required input. Secrets in headers are
replaced by `{name}` before the model sees them. `loom_wrap_rm` removes a bridge and its tools.

## Skills

Skills are text injected into a builder's prompt, so each one is paid on every attempt. One per need, never all at once.

- Roles get theirs by default: implementers `ponytail`; backend `ponytail` + `codebase-design`; frontend and every task that draws a screen `impeccable`; tester `tdd`.
- The planner picks extra ones per task from the dictionary; you can override with `loom_task_set` `skills` (`[]` removes them all).
  `agent-browser` drives a real browser (QA a running app, read live docs): it needs `network: true` and the `agent-browser` CLI.
- Yours, not the builders': `caveman` to answer the human tersely, `grill-with-docs` (`grilling` + `domain-modeling`) to interview before the spec,
  `find-skills` to search skills.sh when the dictionary lacks one. `loom ide` installs them for Claude Code.
- A skill not in the dictionary: `npx skills find <topic>`, then set its name on the task; it is fetched the first time it runs.

## Models and providers

loom does not store API keys. Each CLI uses its own login, and `loom_doctor` never requires a key.
- `cli:` in `.loom/config.yml` picks the default executor: claude, opencode or codex.
- `models: { light, medium, heavy }` maps task weight to model. A model id picks its own CLI: `claude-*` runs on claude,
  `gpt-*` on codex, and `provider/model` on opencode (for example `deepseek/deepseek-chat` or `openrouter/<vendor>/<model>`).
  Different CLIs can be mixed in one config.
- Savings, on by default: a medium task starts on the light model, gets 2 attempts and then escalates to medium.
  A light model on opencode (`deepseek/...`) with a claude medium only runs light tasks: medium ones start on medium.
  `escalate_after: N` or `off` changes that. Nothing escalates to heavy on its own: heavy is for the architect and planner.
- The visual reviewer needs a model that sees images: one that cannot (deepseek) is swapped for another weight or the review turns off with a warning.
  It uses the medium model and sees at most 5 screenshots (`screens_max`), starting with the component
  sheet (`screens_sample`).

## What not to do

- Do not edit `.loom/tasks/*.md` by hand while a run is active. Use `loom_task_set`.
- Do not retry a red task without reading its logs and changing something.
- Do not widen `only_touch` to make a task pass. Split the task or fix the contract.
- Do not launch heavy tools in a loop, and do not poll: wait with `loom_board` `wait: 600`.
- Do not treat green tests as done for interface work. Look at the screenshots.

## Where things live

`.loom/config.yml` holds the config, `.loom/tasks/` the contracts and `.loom/state/` the task states.
`.loom/runs/<id>/` has attempts.jsonl, the attempt logs and the visual-N screenshots.
`.loom/historial/` the closed versions, `.loom/archivo/` their tasks and runs.
`.loom/corridas/` has the run logs, `.loom/bridges/` the bridges, and `~/.loom/ventanas.jsonl` the spend ledger.

## Connecting a client

- Claude Code: `claude mcp add loom -- loom mcp` (or `loom ide`, which wires it and opens the board).
- Codex: `codex mcp add loom -- loom mcp`.
- opencode: in `opencode.json`, `"mcp": {"loom": {"type": "local", "command": ["loom", "mcp"]}}`.
