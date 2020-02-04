/*
Copyright 2019 GramLabs, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package configuration

import (
	"fmt"
	"sort"

	"github.com/redskyops/k8s-experiment/internal/config"
	"github.com/redskyops/k8s-experiment/redskyctl/internal/commander"
	"github.com/spf13/cobra"
)

// TODO Deprecate this in favor of `config view --output env`

// EnvOptions are the options for viewing a configuration as environment variables
type EnvOptions struct {
	// Config is the Red Sky Configuration to view
	Config *config.RedSkyConfig
	// IOStreams are used to access the standard process streams
	commander.IOStreams

	// IncludeController is used to display the full environment
	IncludeController bool
}

// NewEnvCommand creates a new command for viewing a configuration as environment variables
func NewEnvCommand(o *EnvOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Generate environment variables from configuration",
		Long:  "View the Red Sky Configuration file as environment variables",

		PreRun: commander.StreamsPreRun(&o.IOStreams),

		Run: func(cmd *cobra.Command, args []string) {
			err := o.Run()
			commander.CheckErr(cmd, err)
		},
	}

	cmd.Flags().BoolVar(&o.IncludeController, "manager", false, "Generate the manager environment.")

	return cmd
}

func (o *EnvOptions) Run() error {
	env, err := config.LegacyEnvMapping(o.Config, o.IncludeController)
	if err != nil {
		return err
	}

	// Serialize the environment map to a ".env" format
	var keys []string
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		_, _ = fmt.Fprintf(o.Out, "%s=%s\n", k, string(env[k]))
	}

	return nil
}
