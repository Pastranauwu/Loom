package main

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Pastranauwu/loom/internal/idioma"
)

// textoAyuda is the English help of a command; the Spanish one stays in the
// command definition and is used when the language is Spanish.
type textoAyuda struct {
	Short, Long, Example string
	Flags                map[string]string
}

// traducirAyuda replaces the Spanish help of every command by its English
// version unless the language is Spanish. A command with no entry keeps its text.
func traducirAyuda(c *cobra.Command) {
	if !idioma.Es() {
		if t, ok := ayudaEn[c.CommandPath()]; ok {
			if t.Short != "" {
				c.Short = t.Short
			}
			if t.Long != "" {
				c.Long = t.Long
			}
			if t.Example != "" {
				c.Example = t.Example
			}
			for _, fl := range []*pflag.FlagSet{c.Flags(), c.PersistentFlags()} {
				fl.VisitAll(func(f *pflag.Flag) {
					if u, ok := t.Flags[f.Name]; ok {
						f.Usage = u
					}
				})
			}
		}
	}
	for _, s := range c.Commands() {
		traducirAyuda(s)
	}
}

var ayudaEn = map[string]textoAyuda{
	"loom": {Short: "you design the pattern, loom weaves the code with AI agents", Flags: map[string]string{"json": "structured JSON output", "plain": "plain text output, one line per event"}},
	"loom apply": {Short: "apply a declarative spec of tasks (Requirements as Code)",
		Long:  "Reads a declarative spec (loom.spec.yml by default), validates the task\ncontracts, assigns consecutive ids to the ones without, and saves them in\n.loom/tasks/ ready to be verified or run.\n\nWith --run (or -r) it starts the parallel run right away (loom run).",
		Flags: map[string]string{"dry-run": "validate the spec without writing files", "file": "path to the spec file (.yml)", "run": "run the tasks right after applying them"}},
	"loom archive": {Short: "archive the proven spec in the history and empty it",
		Long: "Saves the current spec in .loom/historial/NNNN-<feature>/ next to its result,\nadds a line to .loom/index.md, empties the spec and makes a commit. It only\narchives if the last joint delivery (ship --all) passed every acceptance\ncriterion.\n\nRun it on the PR branch, before the merge."},
	"loom board":        {Short: "task status board"},
	"loom check":        {Short: "run the entry gate on a task"},
	"loom constitution": {Short: "generate the project constitution", Flags: map[string]string{"executor": "opencode or claude", "force": "regenerate if it already exists", "model": "planner model"}},
	"loom dna": {Short: "pick the design DNA your agents follow (hexagonal, zero-deps, crash-early...)",
		Long: "DNA is a set of design archetypes loom injects into every prompt next to the\nconstitution. With no arguments, in a terminal, it opens a picker. Your own\narchetypes live in .loom/dna/<name>.md and override the built-in ones.\n\nBeyond the prompt, the exit gate checks in code what it can: zero-deps blocks\nnew dependencies in manifests; hexagonal and functional-core forbid the domain\nfrom importing adapters or infrastructure."},
	"loom doctor": {Short: "check git, configuration, keys and executors"},
	"loom graph":  {Short: "index the repo into .loom/repo-graph.json (incremental)"},
	"loom heal": {Short: "fix the spec and the code from a failure log",
		Long:  "Reads a stack trace or failure log (--from file, --from - or a pipe), finds\nwhich rule or edge case the spec was missing, adds it to the spec together\nwith an acceptance criterion whose command reproduces the failure, and runs\nthe corrective task in its room until its done_when passes. With --ship it\ndelivers it.",
		Flags: map[string]string{"dry-run": "show the diagnosis without touching the spec or creating the task", "executor": "diagnosis CLI (defaults to config.yml)", "file": "spec to fix (defaults to the repo's)", "from": "file with the stack trace or log (- or a pipe = stdin)", "model": "diagnosis model (defaults to the planner's)", "ship": "deliver the corrective task once it is green"}},
	"loom tutorial": {Short: "a guided tour of loom for new developers"},
	"loom language": {Short: "set the language loom talks to you in"},
	"loom init": {Short: "detect the repo and create .loom/",
		Flags: map[string]string{"cli": "agent CLI: opencode, claude or codex (asks if more than one is found)", "no-skills": "do not fetch the default skills (do it later with loom skills sync)", "tests": "the project's test command, instead of the detected one", "tests-template": "test stack: go, node or python"}},
	"loom clean": {Short: "free the rooms of tasks already on the base branch",
		Long: "Each room is a copy of the repo with its dependencies installed: in a Node\nproject that is over 100 MB per task. clean removes the room and branch of\neach ready task whose work is already on the base branch, plus the\nintegration and delivery ones. It does not touch pending, running or stopped\ntasks, nor contracts, nor the attempt log.\n\nloom archive does this on its own for the tasks of the feature it archives."},
	"loom logs": {Short: "internal detail of a task"},
	"loom map": {Short: "map of modules, contracts and gates without opening code",
		Long:    "Draws the system at a high level from what loom already knows: the modules,\nthe public contracts (exposes) each one offers, who consumes whom (uses and\nimports), the active DNA and the state of the gates. It opens no source\nfile: it comes from the repo graph and the task contracts.\n\nIn a terminal it is a tree of cards; --mermaid exports a diagram for Markdown\nor a PR; --json, the full structure.",
		Example: "  loom map\n  loom map --mermaid > docs/architecture.md\n  loom map --depth 3 --all\n  loom view --json",
		Flags:   map[string]string{"all": "include modules with no tasks or contracts", "depth": "path segments that name a module", "mermaid": "Mermaid diagram instead of the tree"}},
	"loom models": {Short: "pick the model for each weight (light, medium, heavy) from the CLI catalog"},
	"loom plan": {Short: "turn a request into task contracts",
		Flags: map[string]string{"approve": "create the tasks without asking", "executor": "opencode or claude (defaults to the first available)", "export-spec": "export the proposal as a declarative spec file (e.g. loom.spec.yml)", "model": "planner model (defaults to its own)"}},
	"loom ps": {Short: "show the state of tasks and active rooms (compose style)",
		Long: "Lists every task of the project with its current state (pending, running,\nready, stopped), assigned agent and room/port if it is active."},
	"loom repair": {Short: "reopen the task that broke the last joint delivery",
		Long: "When ship --all stops while testing the whole set (integrated or acceptance),\nrepair reopens the responsible task with the failure in its contract and runs\nit again in its room. Without an id it picks the task that touches the code\nthe failing test covers; with an id it reopens that one.\n\nNo new agent is created: whoever repairs is the task that made the change,\nwith its contract in front, and with the medium model at least. Deciding\nwhether the code or the old test is wrong is not a job for the light model."},
	"loom report": {Short: "project metrics"},
	"loom run": {Short: "run the pending tasks in parallel",
		Flags: map[string]string{"agents": "tasks in parallel (0 = automatic, up to 16, and 3 with a paid model)", "background": "detach the run from the terminal and return control; it keeps running if you close it", "executor": "opencode or claude (defaults to the first available)", "model": "executor model (defaults to its own)", "retry": "also rerun the stopped tasks, reusing their room"}},
	"loom ship": {Short: "exit gate and PR",
		Long:  "With an id it delivers that task in its own PR.\n\nWith --all it delivers in ONE PR every task that is ready: each one passes its\nexit gate on its own and contributes exactly one commit, in dependency order.\nIt is the way not to end up with N pull requests that step on each other.",
		Flags: map[string]string{"all": "deliver every ready task in a single PR", "dry-run": "run the gate without opening the PR", "local": "leave the PR in the repo (branch and description) even with a remote; same as delivery: local in config.yml", "merge": "review and, if it asks for no changes, rebase-merge the PR without waiting for you", "review": "a model reviews the diff and leaves the report on the PR; you approve", "title": "title of the joint PR (defaults to the first task's)"}},
	"loom sketch": {Short: "interview you about your idea and write the spec",
		Long:    "A model asks four questions about the edge cases, rules and failures your\nidea does not mention; you answer each in one line and loom writes\nloom.spec.yml from the interview.",
		Example: "  loom sketch \"a job queue with retries\"\n  loom sketch -o specs/queues.yml \"queues\"",
		Flags:   map[string]string{"executor": "interviewer CLI (defaults to config.yml)", "force": "overwrite an existing spec", "model": "interviewer model (defaults to the planner's)", "out": "where to write the spec (defaults to loom.spec.yml)"}},
	"loom skills":      {Short: "manage the real skills injected into each agent's prompt"},
	"loom skills sync": {Short: "fetch with npx the skill packages the catalog agents use"},
	"loom standup":     {Short: "data report of the running tasks"},
	"loom stop":        {Short: "stop the current run and its agents"},
	"loom task":        {Short: "manual task management"},
	"loom task add": {Short: "create a new task with a consecutive id",
		Example: "  loom task add \"export customers to CSV\"\n  loom task add \"design the architecture\" --agent architect",
		Flags:   map[string]string{"agent": "agent assigned to the task (declared in config.yml)"}},
	"loom task check": {Short: "run the entry gate on a task"},
	"loom task edit":  {Short: "open the task in $EDITOR"},
	"loom task list":  {Short: "list the tasks"},
	"loom task rm":    {Short: "delete a task"},
	"loom task seal": {Short: "seal a hand-written hidden suite, spending no model",
		Long:    "Seals tests you wrote yourself, or that an expensive model generated apart\nfrom the implementer's loop, instead of leaving them to the blind examiner.\n\nThe --visible suite is copied into the task's room when it runs: the\nimplementer sees it and it is its acceptance criterion. The --hidden suite is\nsealed with a hash and only runs in the exit gate (loom ship), which cannot\ntell whether a human or the examiner wrote it.\n\nFiles are read from disk when sealing, so you can delete them afterwards:\nwhat is sealed no longer depends on them.",
		Example: "  loom task seal T-001 --visible tests/visible_test.go --hidden tests/hidden_test.go\n  loom task seal T-001 --visible v.py --hidden o.py --force",
		Flags:   map[string]string{"force": "replace an already sealed suite", "hidden": "file with the tests that are sealed and only run in ship", "visible": "file with the tests the implementer DOES see"}},
	"loom up": {Short: "from a request to a clean PR, with no steps in between",
		Long:    "Chains all of loom's work in a single command.\n\nWith a request in natural language it splits it into tasks (loom plan), runs\nthem in parallel in isolated rooms (loom run) and, with --ship, delivers them\nin ONE clean pull request (loom ship --all).\n\nWithout a request it applies the repo's spec (loom.spec.yml) if there is one\nand runs the pending tasks.\n\nYou do not need loom init: up sets up whatever is missing (git repo, .loom,\ninitial commit, executor, models, test command) and only asks when it cannot\nresolve it on its own.",
		Example: "  loom up \"a Go CLI that wakes machines with wake-on-lan\" --ship\n  loom up \"fix the login with accents\" --merge\n  loom up --agents 4 --ship\n  loom up -f specs/auth.yml",
		Flags:   map[string]string{"agents": "number of parallel workers (0 = automatic, up to 16, and 3 with a paid model)", "background": "detach the whole chain from the terminal and return control; it keeps running if you close it", "executor": "opencode or claude", "file": "path to the spec file (.yml)", "local": "delivery stays as a local PR (branch and description in the repo) even with a remote", "merge": "besides reviewing, merge the PR if the reviewer asks for no changes", "model": "force a model for every task", "retry": "also rerun the stopped tasks", "review": "besides delivering, a model reviews the diff and leaves the report on the PR", "ship": "when done, deliver every ready task in a single PR", "title": "PR title (defaults to the request)"}},
	"loom usage": {Short: "spend by windows (5h, weekly, monthly) and budget", Flags: map[string]string{"probe": "force a live query of Claude's real utilization (ignores the cache)"}},
}
