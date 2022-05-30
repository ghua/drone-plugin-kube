package main

import (
	"context"
	"errors"
	"io/ioutil"
	v1 "k8s.io/api/batch/v1"
	"k8s.io/client-go/kubernetes"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/aymerick/raymond"
	appV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	v1BetaV1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/client-go/kubernetes/scheme"
)

type (
	// KubeConfig -- Contains connection settings for Kube client
	KubeConfig struct {
		Ca                    string
		Server                string
		Token                 string
		Namespace             string
		InsecureSkipTLSVerify bool
	}
	// Plugin -- Contains config for plugin
	Plugin struct {
		Template   string
		KubeConfig KubeConfig
	}
)

// Exec -- Runs plugin
func (p Plugin) Exec() error {
	if p.KubeConfig.Server == "" {
		return errors.New("PLUGIN_SERVER is not defined")
	}
	if p.KubeConfig.Token == "" {
		return errors.New("PLUGIN_TOKEN is not defined")
	}
	if p.KubeConfig.Ca == "" {
		return errors.New("PLUGIN_CA is not defined")
	}
	if p.Template == "" {
		return errors.New("PLUGIN_TEMPLATE, or template must be defined")
	}
	// Make map of environment variables set by Drone
	envCtx := make(map[string]string)
	pluginEnv := os.Environ()
	for _, value := range pluginEnv {
		re := regexp.MustCompile(`^PLUGIN_(.*)=(.*)`)
		if re.MatchString(value) {
			matches := re.FindStringSubmatch(value)
			key := strings.ToLower(matches[1])
			envCtx[key] = matches[2]
		}

		re = regexp.MustCompile(`^DRONE_(.*)=(.*)`)
		if re.MatchString(value) {
			matches := re.FindStringSubmatch(value)
			key := strings.ToLower(matches[1])
			envCtx[key] = matches[2]
		}
	}

	// Grab template from filesystem
	raw, err := ioutil.ReadFile(p.Template)
	if err != nil {
		log.Print("⛔️ Error reading template file:")
		return err
	}

	// Parse template
	templateYaml, err := raymond.Render(string(raw), envCtx)
	if err != nil {
		return err
	}

	// Connect to Kubernetes
	clientset, err := p.CreateKubeClient()
	if err != nil {
		return err
	}

	templateYamlParts := strings.Split(templateYaml, "---")
	for _, templateYamlPart := range templateYamlParts {
		templateYamlPart = strings.TrimSpace(templateYamlPart)
		if len(templateYamlPart) == 0 {
			continue
		}

		err = p.handleYamlConfig(templateYamlPart, clientset)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p Plugin) handleYamlConfig(templateYaml string, clientset *kubernetes.Clientset) error {
	// Decode
	kubernetesObject, _, err := scheme.Codecs.UniversalDeserializer().Decode([]byte(templateYaml), nil, nil)
	if err != nil {
		log.Print("⛔️ Error decoding template into valid Kubernetes object:")
		return err
	}

	ctx := context.Background()

	switch o := kubernetesObject.(type) {
	case *appV1.Deployment:
		log.Print("📦 Resource type: Deployment")
		if p.KubeConfig.Namespace == "" {
			p.KubeConfig.Namespace = o.Namespace
		}

		err = CreateOrUpdateDeployment(ctx, clientset, p.KubeConfig.Namespace, o)
		if err != nil {
			return err
		}

		// Watch for successful update
		log.Print("📦 Watching deployment until no unavailable replicas.")
		state, watchErr := waitUntilDeploymentSettled(ctx, clientset, p.KubeConfig.Namespace, o.ObjectMeta.Name, 120)
		log.Printf("%s", state)
		return watchErr
	case *coreV1.ConfigMap:
		if p.KubeConfig.Namespace == "" {
			p.KubeConfig.Namespace = o.Namespace
		}

		log.Print("📦 Resource type: ConfigMap")
		err = ApplyConfigMap(ctx, clientset, p.KubeConfig.Namespace, o)
	case *coreV1.Service:
		if p.KubeConfig.Namespace == "" {
			p.KubeConfig.Namespace = o.Namespace
		}

		log.Print("Resource type: Service")
		err = ApplyService(ctx, clientset, p.KubeConfig.Namespace, o)
	case *v1BetaV1.Ingress:
		if p.KubeConfig.Namespace == "" {
			p.KubeConfig.Namespace = o.Namespace
		}

		log.Print("Resource type: Ingress")
		err = ApplyIngress(ctx, clientset, p.KubeConfig.Namespace, o)
	case *v1.CronJob:
		if p.KubeConfig.Namespace == "" {
			p.KubeConfig.Namespace = o.Namespace
		}
		log.Print("Resource type: CronJob")
		err = ApplyCronJob(ctx, clientset, p.KubeConfig.Namespace, o)
	default:
		return errors.New("⛔️ This plugin doesn't support that resource type")
	}

	return err
}
