package main

type stepKind int

const (
	stepDoctor stepKind = iota
	stepNetworkMode
	stepTLS
	stepAuth
	stepProject
	stepSaveConfig
	stepCompletion
	stepServiceInstall
	stepSummary
)

type step struct {
	kind  stepKind
	title string
	desc  string
}

func buildSteps(networkMode bool) []step {
	steps := []step{
		{stepDoctor, "Checking dependencies", "Verifying required tools are installed"},
		{stepNetworkMode, "Network mode", "How will you access Agentique?"},
	}
	if networkMode {
		steps = append(steps,
			step{stepTLS, "TLS configuration", "How will you handle HTTPS?"},
			step{stepAuth, "Authentication", "Protect access with passkeys?"},
		)
	}
	steps = append(steps,
		step{stepProject, "First project", "Path to your first project (optional)"},
		step{stepSaveConfig, "Save configuration", "Writing config file"},
		step{stepCompletion, "Shell completions", "Install tab completion for your shell?"},
		step{stepServiceInstall, "System service", "Install as a system service?"},
		step{stepSummary, "Setup complete", ""},
	)
	return steps
}
