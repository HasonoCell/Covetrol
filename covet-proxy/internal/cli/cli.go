package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"covetrol/covet-proxy/internal/agent"
	"covetrol/covet-proxy/internal/proxy"
	"covetrol/covet-proxy/internal/store"
	networkingv1 "covetrol/pkg/apis/networking/v1"
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
	nodeAgent := agent.NewCoveletCLI(
		filepath.Join(cwd, "covelet-bin"),
		cwd,
	)
	proxy := proxy.New(nodeAgent)

	switch args[0] {
	case "apply":
		return apply(args[1:])
	case "get":
		return get(args[1:])
	case "delete":
		return deleteService(args[1:])
	case "serve":
		return serve(proxy, args[1:])
	case "help", "-h", "--help":
		return usageError("")
	default:
		return usageError(fmt.Sprintf("unknown command %q", args[0]))
	}
}

func apply(args []string) error {
	flagset := flag.NewFlagSet("apply", flag.ContinueOnError)
	flagset.SetOutput(os.Stderr)
	filePath := flagset.String("f", "", "service file")
	if err := flagset.Parse(args); err != nil {
		return fmt.Errorf("parse apply flags: %w", err)
	}
	if *filePath == "" {
		return fmt.Errorf("usage: covet-proxy apply -f <service.yaml>")
	}
	svc, err := loadService(*filePath)
	if err != nil {
		return err
	}
	return store.SaveService(svc)
}

func get(args []string) error {
	if len(args) != 2 || args[0] != "service" {
		return fmt.Errorf("usage: covet-proxy get service <name>")
	}
	svc, err := store.LoadService(args[1])
	if err != nil {
		return err
	}
	return printJSON(svc)
}

func deleteService(args []string) error {
	if len(args) != 2 || args[0] != "service" {
		return fmt.Errorf("usage: covet-proxy delete service <name>")
	}
	return store.RemoveService(args[1])
}

func serve(proxy *proxy.Proxy, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: covet-proxy serve <service-name>")
	}
	svc, err := store.LoadService(args[0])
	if err != nil {
		return err
	}
	return proxy.Serve(svc)
}

func loadService(path string) (networkingv1.Service, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return networkingv1.Service{}, fmt.Errorf("read service file %q: %w", path, err)
	}

	var svc networkingv1.Service
	if err := yaml.Unmarshal(data, &svc); err == nil {
		return svc, nil
	}
	if err := json.Unmarshal(data, &svc); err != nil {
		return networkingv1.Service{}, fmt.Errorf("decode service file %q as yaml or json: %w", path, err)
	}
	return svc, nil
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
	usage := "usage:\n  covet-proxy apply -f <service.yaml>\n  covet-proxy get service <name>\n  covet-proxy delete service <name>\n  covet-proxy serve <name>"
	if prefix == "" {
		return errors.New(usage)
	}
	return fmt.Errorf("%s\n\n%s", prefix, usage)
}
