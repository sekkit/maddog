package contextpack

import "strings"

type profileOutput struct {
	content         string
	quality         ParseQuality
	qualityReason   string
	unparsedLines   int
	unparsedSamples []string
}

type shellProfile struct {
	id       string
	strategy string
	match    func(commandDescriptor) bool
	parse    func(string, int) profileOutput
}

var builtinShellProfiles = []shellProfile{
	{id: "rg", strategy: "rg-file-sampling", match: matchRipgrepProfile, parse: parseRipgrepProfileOutput},
	{id: "git-status", strategy: "git-status-summary", match: matchGitStatusProfile, parse: parseGitStatusProfileOutput},
	{id: "git-diff", strategy: "git-diff-summary", match: matchGitDiffProfile, parse: parseGitDiffProfileOutput},
	{id: "go-test", strategy: "go-test-failure", match: matchGoTestProfile, parse: parseGoTestProfileOutput},
	{id: "npm-test", strategy: "npm-test-failure", match: matchNPMTestProfile, parse: parseNPMTestProfileOutput},
	{id: "npm-build", strategy: "npm-build-error", match: matchNPMBuildProfile, parse: parseNPMBuildProfileOutput},
}

func matchRipgrepProfile(desc commandDescriptor) bool {
	return desc.Executable == "rg" && desc.OutputShape == outputShapeRipgrepMatches
}

func matchGitStatusProfile(desc commandDescriptor) bool {
	return desc.Executable == "git" && desc.Subcommand == "status" && desc.OutputShape == outputShapeGitStatusShort
}

func matchGitDiffProfile(desc commandDescriptor) bool {
	return desc.Executable == "git" && desc.Subcommand == "diff" && desc.OutputShape == outputShapeGitDiffPatch
}

func matchGoTestProfile(desc commandDescriptor) bool {
	return desc.Executable == "go" && desc.Subcommand == "test" && desc.OutputShape == outputShapeGoTestText
}

func matchNPMTestProfile(desc commandDescriptor) bool {
	return matchesPackageScript(desc, "test")
}

func matchNPMBuildProfile(desc commandDescriptor) bool {
	return matchesPackageScript(desc, "build")
}

func matchesPackageScript(desc commandDescriptor, script string) bool {
	if desc.Executable != "npm" && desc.Executable != "pnpm" && desc.Executable != "yarn" {
		return false
	}
	return desc.OutputShape == outputShapePackageText && desc.Subcommand == script
}

func hasAnyFlag(args []string, flags ...string) bool {
	for _, arg := range args {
		for _, flag := range flags {
			if arg == flag || strings.HasPrefix(arg, flag+"=") {
				return true
			}
		}
	}
	return false
}
