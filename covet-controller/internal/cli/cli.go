package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"covetrol/covet-controller/internal/agent"
	"covetrol/covet-controller/internal/controller"
	appsv1 "covetrol/pkg/apis/apps/v1"
	"gopkg.in/yaml.v3"
)

func Run(args []string) error {
	if len(args) == 0 {
		return usageError("")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	ctrl := controller.New(agent.NewCoveletCLI(
		filepath.Join(cwd, "covelet-bin"),
		cwd,
	))

	switch args[0] {
	case "apply":
		return apply(ctrl, args[1:])
	case "get":
		return get(ctrl, args[1:])
	case "reconcile":
		return reconcile(ctrl, args[1:])
	case "delete":
		return deleteReplicaSet(ctrl, args[1:])
	case "help", "-h", "--help":
		return usageError("")
	default:
		return usageError(fmt.Sprintf("unknown command %q", args[0]))
	}
}

func apply(ctrl *controller.Controller, args []string) error {
	flagset := flag.NewFlagSet("apply", flag.ContinueOnError)
	flagset.SetOutput(os.Stderr)
	filePath := flagset.String("f", "", "replica set file")
	if err := flagset.Parse(args); err != nil {
		return fmt.Errorf("parse apply flags: %w", err)
	}
	if *filePath == "" {
		return fmt.Errorf("usage: covet-controller apply -f <replicaset.yaml>")
	}

	rs, err := loadReplicaSet(*filePath)
	if err != nil {
		return err
	}
	return ctrl.Apply(rs)
}

func get(ctrl *controller.Controller, args []string) error {
	if len(args) != 2 || args[0] != "rs" {
		return fmt.Errorf("usage: covet-controller get rs <name>")
	}
	rs, err := ctrl.Get(args[1])
	if err != nil {
		return err
	}
	return printJSON(rs)
}

func reconcile(ctrl *controller.Controller, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: covet-controller reconcile <name>")
	}
	status, err := ctrl.Reconcile(args[0])
	if err != nil {
		return err
	}
	return printJSON(status)
}

func deleteReplicaSet(ctrl *controller.Controller, args []string) error {
	if len(args) != 2 || args[0] != "rs" {
		return fmt.Errorf("usage: covet-controller delete rs <name>")
	}
	return ctrl.Delete(args[1])
}

func loadReplicaSet(path string) (appsv1.ReplicaSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return appsv1.ReplicaSet{}, fmt.Errorf("read replica set file %q: %w", path, err)
	}

	var rs appsv1.ReplicaSet
	if err := yaml.Unmarshal(data, &rs); err == nil {
		return rs, nil
	}
	if err := json.Unmarshal(data, &rs); err != nil {
		return appsv1.ReplicaSet{}, fmt.Errorf("decode replica set file %q as yaml or json: %w", path, err)
	}
	return rs, nil
}

func printJSON(value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(data, '\n'))
	return err
}

func usageError(prefix string) error {
	usage := "usage:\n  covet-controller apply -f <replicaset.yaml>\n  covet-controller get rs <name>\n  covet-controller reconcile <name>\n  covet-controller delete rs <name>"
	if prefix == "" {
		return errors.New(usage)
	}
	return fmt.Errorf("%s\n\n%s", prefix, usage)
}
