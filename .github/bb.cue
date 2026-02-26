package github

import (
	"tool/file"

	"cue.dev/x/githubactions@v0:githubactions"
)

_mainBranch: "main"
_checkoutStep: {uses: "actions/checkout@v4"}
_miseStep: {uses: "jdx/mise-action@v3"}
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
						set -euo pipefail

						max_tries=5
						for i in $(seq 1 $max_tries); do
						  if go get cuelang.org/go@master; then
						    break
						  fi
						  if [ $i -eq $max_tries ]; then
						    echo "go get failed after $max_tries attempts"
						    exit 1
						  fi
						  echo "go get failed, retrying in 10s..."
						  sleep 10
						done

						go mod tidy

						if git diff --quiet go.mod go.sum; then
						  echo "changed=false" >> "$GITHUB_OUTPUT"
						else
						  cue_sha=$(grep 'cuelang.org/go' go.mod | grep -oP '[0-9a-f]{12}$')
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
	on: push: branches: [_mainBranch]
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
					uses: "actions/upload-artifact@v4"
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
					uses: "actions/download-artifact@v4"
					with: {
						path:             "dist"
						pattern:          "bbcue-*"
						"merge-multiple": "true"
					}
				},
				{
					name: "Create release"
					uses: "softprops/action-gh-release@v2"
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
