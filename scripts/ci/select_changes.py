"""Select CI work from the complete PR/push diff; uncertain ranges run all checks."""

import json
import os
from pathlib import Path
import subprocess


FLAGS = ("backend", "frontend", "protocol", "windows", "generation", "dependencies")


def select(paths, full=False):
    if full:
        return dict.fromkeys(FLAGS, True)
    result = dict.fromkeys(FLAGS, False)
    for path in paths:
        if path == "Makefile" or path.startswith((".github/", "scripts/ci/")):
            return dict.fromkeys(FLAGS, True)
        if path.endswith(".md") or path.startswith(("docs/", ".agent/")):
            continue
        dependency = path in ("go.mod", "go.sum", "llm/go.mod", "llm/go.sum", "cmd/schema/go.mod", "cmd/schema/go.sum", ".go-version")
        backend = dependency or path.endswith(".go") or path.startswith(("conf/", "deploy/", "scripts/verify-go-"))
        frontend = path.startswith(("frontend/", "scripts/pack-frontend", "internal/server/static/"))
        protocol = dependency or path.startswith(("llm/", "internal/server/orchestrator/", "internal/server/api/", "internal/contexts/", "scripts/codex-probes/"))
        generation = dependency or path.startswith(("internal/ent/", "internal/server/gql/", "internal/objects/", "cmd/schema/")) or path == "gqlgen.yml"
        if not (backend or frontend or protocol or generation):
            # A new owning directory must not silently bypass validation.
            return dict.fromkeys(FLAGS, True)
        result["backend"] |= backend or generation
        result["frontend"] |= frontend
        result["protocol"] |= protocol
        result["windows"] |= protocol or path.startswith(("cmd/axonhub/", "internal/server/biz/", "internal/ent/"))
        result["generation"] |= generation
        result["dependencies"] |= dependency
    return result


def main():
    event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text())
    name = os.environ.get("GITHUB_EVENT_NAME", "")
    base = event.get("pull_request", {}).get("base", {}).get("sha") or event.get("before", "")
    full = name == "workflow_dispatch" or not base or set(base) == {"0"}
    paths = []
    if not full:
        try:
            raw = subprocess.check_output(["git", "diff", "--name-only", "-z", base, "HEAD"])
            paths = [p.decode("utf-8", errors="replace") for p in raw.split(b"\0") if p]
        except subprocess.CalledProcessError:
            full = True
    selected = select(paths, full)
    with open(os.environ["GITHUB_OUTPUT"], "a") as output:
        for key, value in selected.items():
            output.write(f"{key}={str(value).lower()}\n")
    print(json.dumps({"full": full, "changed_files": len(paths), "checks": selected}))


if __name__ == "__main__":
    main()
