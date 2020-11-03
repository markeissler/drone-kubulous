package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"reflect"
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
		Template      string
		KubeConfig    KubeConfig
		ConfigMapFile string // Optional
	}
)

const defaultNamespace = "default"

// Exec -- Runs plugin
func (p Plugin) Exec() error {
	if p.KubeConfig.Server == "" {
		return errors.New("PLUGIN_SERVER or settings.server must be defined")
	}
	if p.KubeConfig.Token == "" {
		return errors.New("PLUGIN_TOKEN or settings.token must be defined")
	}
	if p.KubeConfig.Ca == "" {
		return errors.New("PLUGIN_CA or settings.ca must be defined")
	}
	if p.Template == "" {
		return errors.New("PLUGIN_TEMPLATE or settings.template must be defined")
	}
	// Make map of environment variables set by Drone
	ctx := make(map[string]string)
	pluginEnv := os.Environ()
	for _, value := range pluginEnv {
		re := regexp.MustCompile(`^PLUGIN_(.*)=(.*)`)
		if re.MatchString(value) {
			matches := re.FindStringSubmatch(value)
			key := strings.ToLower(matches[1])
			ctx[key] = matches[2]
		}

		re = regexp.MustCompile(`^DRONE_(.*)=(.*)`)
		if re.MatchString(value) {
			matches := re.FindStringSubmatch(value)
			key := strings.ToLower(matches[1])
			ctx[key] = matches[2]
		}
	}

	// Grab template from filesystem
	raw, err := ioutil.ReadFile(p.Template)
	if err != nil {
		log.Print("⛔️ Error reading template file:")
		return err
	}

	// Parse template
	templateYaml, err := raymond.Render(string(raw), ctx)
	if err != nil {
		return err
	}

	// Connect to Kubernetes
	clientset, err := p.CreateKubeClient()
	if err != nil {
		return err
	}

	// Decode
	kubernetesObject, _, err := scheme.Codecs.UniversalDeserializer().Decode([]byte(templateYaml), nil, nil)
	if err != nil {
		log.Print("⛔️ Error decoding template into valid Kubernetes object:")
		return err
	}

	// Settings namespace takes precedence over template namespace
	p.KubeConfig.Namespace = getStructFieldStringValueOrDefault(kubernetesObject, "Namespace", defaultNamespace)

	switch o := kubernetesObject.(type) {
	case *appV1.Deployment:
		log.Print("📦 Resource type: Deployment")

		err = CreateOrUpdateDeployment(clientset, p.KubeConfig.Namespace, o)
		if err != nil {
			return err
		}

		// Watch for successful update
		log.Print("📦 Watching deployment until no unavailable replicas.")
		state, watchErr := waitUntilDeploymentSettled(clientset, p.KubeConfig.Namespace, o.ObjectMeta.Name, 120)
		log.Printf("%s", state)
		return watchErr
	case *coreV1.ConfigMap:
		log.Print("📦 Resource type: ConfigMap")

		err = ApplyConfigMapFromFile(clientset, p.KubeConfig.Namespace, o, p.ConfigMapFile)
	case *coreV1.Service:
		log.Print("📦 Resource type: Service")

		err = CreateOrUpdateService(clientset, p.KubeConfig.Namespace, o)
		if err != nil {
			return err
		}

		// Watch for successful update
		log.Print("📦 Watching service until load balancer has been assigned.")
		state, watchErr := waitUntilServiceSettled(clientset, p.KubeConfig.Namespace, o.ObjectMeta.Name, 120)
		log.Printf("%s", state)
		return watchErr
	case *v1BetaV1.Ingress:
		log.Print("Resource type: Ingress")

		err = ApplyIngress(clientset, p.KubeConfig.Namespace, o)
	default:
		err = errors.New("⛔️ This plugin doesn't support that resource type")
		err = errors.New(fmt.Sprintf("resource type: %v", reflect.TypeOf(kubernetesObject)))
		return err
	}
	return err
}

func getStructFieldStringValueOrDefault(i interface{}, fieldName string, defValue string) string {
	f := reflect.ValueOf(i).Elem().FieldByName(fieldName)
	if f.IsValid() {
		v := reflect.Indirect(f)
		if v.Kind() == reflect.String {
			return v.String()
		}
	}
	return defValue
}
