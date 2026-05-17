package github

import (
	"tool/file"

	"cue.dev/x/githubactions@v0:githubactions"
)

_mainBranch: "main"
_checkoutStep: {uses: "actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd"} // v6
_miseStep: {uses: "jdx/mise-action@1648a7812b9aeae629881980618f079932869151"}          // v4
_runner:  "ubuntu-latest"
_version: "0.0.0"

// --- CI Workflow ---

_ci: githubactions.#Workflow & {
	name: "CI"
	on: {
		push: branches: [_mainBranch]
		pull_request: branches: [_mainBranch]
	}
	jobs: {
		test: {
			"runs-on": _runner
			steps: [
				_checkoutStep,
				_miseStep,
				{run: "just ci"},
			]
		}
	}
}

// --- Nightly CUE Update Workflow ---

_updateCue: githubactions.#Workflow & {
	name: "Update dependency: CUE"
	on: {
		schedule: [{cron: "0 3 * * *"}]
		workflow_dispatch: {}
	}
	permissions: {
		contents: "write"
	}
	jobs: {
		update: {
			"runs-on": _runner
			steps: [
				_checkoutStep,
				_miseStep,
				{
					name: "Update CUE dependency"
					id:   "update"
					run: ##"""
						set -euxo pipefail

						./scripts/retry_with_sleep.sh 5 10 go get cuelang.org/go@master
						go mod tidy

						if git diff --quiet go.mod go.sum; then
						  echo "changed=false" >> "$GITHUB_OUTPUT"
						else
						  cue_sha=$(go list -m -f '{{.Version}}' cuelang.org/go | grep -oP '[0-9a-f]{12}$')
						  echo "changed=true" >> "$GITHUB_OUTPUT"
						  echo "cue_sha=${cue_sha}" >> "$GITHUB_OUTPUT"
						fi
						"""##
				},
				{
					name: "Run tests"
					if:   "steps.update.outputs.changed == 'true'"
					run: ##"""
						just test
						"""##
				},
				{
					name: "Commit and push to main"
					if:   "steps.update.outputs.changed == 'true'"
					run: ##"""
						git config user.name "github-actions[bot]"
						# 41898282 is the GitHub user ID of the github-actions[bot] account
						git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
						git add go.mod go.sum
						git commit -m 'chore(deps): bump cuelang.org/go to ${{ steps.update.outputs.cue_sha }}'
						git push
						"""##
				},
				{
					name: "Trigger release workflow (repository_dispatch)"
					if:   "steps.update.outputs.changed == 'true'"
					env: {
						REPO:  "${{ github.repository }}"
						TOKEN: "${{ secrets.GITHUB_TOKEN }}"
					}
					run: ##"""
						set -euxo pipefail

						RETRIES=5
						SLEEP=5

						for i in $(seq 1 $RETRIES); do
							rc=0
							http_code=$(curl -sS -o /dev/null -w "%{http_code}" -X POST "https://api.github.com/repos/${REPO}/dispatches" \
								-H "Accept: application/vnd.github+json" \
								-H "Authorization: token ${TOKEN}" \
								-d '{"event_type":"cue-updated"}' ) || rc=$?

							rc=${rc:-0}

							if [ "$rc" -ne 0 ]; then
								echo "curl failed with exit code $rc"
							elif [ "$http_code" -ge 200 ] && [ "$http_code" -lt 300 ]; then
								echo "repository_dispatch sent (HTTP $http_code)"
								break
							elif [ "$http_code" -ge 500 ]; then
								echo "server error (HTTP $http_code), will retry"
							else
								echo "unexpected response (HTTP $http_code), not retrying"
								exit 1
							fi

							if [ "$i" -lt "$RETRIES" ]; then
								sleep $((SLEEP * i))
								echo "retrying (attempt $((i+1))/$RETRIES)"
								continue
							else
								echo "all retries failed"
								exit 1
							fi
						done
						"""##
				},
			]
		}
	}
}

// --- Release Workflow ---

_build_targets: {
	darwin: ["amd64", "arm64"]
	linux: ["amd64", "arm64", "riscv64"]
}


_release: githubactions.#Workflow & {
	name: "Release"
	on: {
		push: branches: [_mainBranch]
		repository_dispatch: { types: ["cue-updated"] }
	}
	permissions: contents: "write"
	jobs: {
		build: {
			"runs-on": _runner
			strategy: matrix: {
				include: [
					for os, archs in _build_targets for arch in archs {
						goos:   os
						goarch: arch
					},
				]
			}
			steps: [
				_checkoutStep,
				_miseStep,
				{
					name: "Build"
					run:  "go build -ldflags='-s -w' -o bbcue-${{ matrix.goos }}-${{ matrix.goarch }} ./cmd/bbcue"
					env: {
						CGO_ENABLED: "0"
						GOOS:        "${{ matrix.goos }}"
						GOARCH:      "${{ matrix.goarch }}"
					}
				},
				{
					name: "Upload artifact"
					uses: "actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a" // v7
					with: {
						name: "bbcue-${{ matrix.goos }}-${{ matrix.goarch }}"
						path: "bbcue-${{ matrix.goos }}-${{ matrix.goarch }}"
					}
				},
			]
		}
		release: {
			needs:     "build"
			"runs-on": _runner
			steps: [
				_checkoutStep,
				{
					name: "Generate tag"
					id:   "tag"
					run:  """
						sha=$(git rev-parse --short HEAD)
						ts=$(git show -s --format=%cd --date=format:'%Y%m%d%H%M%S' HEAD)
						echo "tag=v\(_version)-${ts}-${sha}" >> "$GITHUB_OUTPUT"
						"""
				},
				{
					name: "Download artifacts"
					uses: "actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c" // v8
					with: {
						path:             "dist"
						pattern:          "bbcue-*"
						"merge-multiple": "true"
					}
				},
				{
					name: "Create release"
					uses: "softprops/action-gh-release@b4309332981a82ec1c5618f44dd2e27cc8bfbfda" // v3
					with: {
						tag_name:               "${{ steps.tag.outputs.tag }}"
						name:                   "bbcue ${{ steps.tag.outputs.tag }}"
						generate_release_notes: true
						files:                  "dist/bbcue-*"
					}
				},
			]
		}
	}
}

_workflowsDir: "workflows"
_cleanWorkflows: file.RemoveAll & {
	path: _workflowsDir
}

bbcue: {
	_after: _cleanWorkflows

	"\(_workflowsDir)/ci.yaml": {
		format:  "yaml"
		content: _ci
	}
	"\(_workflowsDir)/update-cue.yaml": {
		format:  "yaml"
		content: _updateCue
	}
	"\(_workflowsDir)/release.yaml": {
		format:  "yaml"
		content: _release
	}
}
