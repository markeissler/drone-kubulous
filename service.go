package main

import (
	"fmt"
	"log"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const serviceUpdateAttempts = 10

// CreateOrUpdateService -- Checks if service already exists, updates if it does, creates if it doesn't
func CreateOrUpdateService(clientset *kubernetes.Clientset, namespace string, service *corev1.Service) error {
	existingService, err := serviceExists(clientset, namespace, service.Name)
	if existingService != nil {
		log.Printf("📦 Found existing service '%s'. Removing.", service.Name)
		ok, err := deleteService(clientset, namespace, service)
		if !ok && err != nil {
			return err
		}
	}

	log.Printf("📦 Creating new service '%s'. Updating.", service.Name)
	_, err = clientset.CoreV1().Services(namespace).Create(service)
	return err
}

// deleteService -- Deletes an existing service
func deleteService(clientset *kubernetes.Clientset, namespace string, service *corev1.Service) (bool, error) {
	err := clientset.CoreV1().Services(namespace).Delete(service.Name, &meta.DeleteOptions{})
	if err != nil {
		statusError, ok := err.(*errors.StatusError)
		if ok == true && statusError.Status().Code == 404 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// serviceExists -- Service already exists in Kubernetes
func serviceExists(clientset *kubernetes.Clientset, namespace string, name string) (*corev1.Service, error) {
	service, err := clientset.CoreV1().Services(namespace).Get(name, meta.GetOptions{})
	if err != nil {
		statusError, ok := err.(*errors.StatusError)
		if ok == true && statusError.Status().Code == 404 {
			return nil, nil
		}
		return nil, err
	}
	return service, nil
}

// waitUntilServiceSettled -- Waits until ready, failure or timeout
func waitUntilServiceSettled(clientset *kubernetes.Clientset, namespace string, name string, timeout int64) (state string, err error) {
	fieldSelector := strings.Join([]string{"metadata.name", name}, "=")
	watchOptions := meta.ListOptions{
		FieldSelector: fieldSelector,
		Watch:         true,
	}
	watcher, err := clientset.CoreV1().Services(namespace).Watch(watchOptions)
	if err != nil {
		return "", err
	}

	liveService, err := clientset.CoreV1().Services(namespace).Get(name, meta.GetOptions{})
	ingressList := make([]string, 0)
	log.Printf("📦 Waiting for load balancer assignment.")
	if len(liveService.Status.LoadBalancer.Ingress) > 0 {
		for _, i := range liveService.Status.LoadBalancer.Ingress {
			ingress := i.IP
			if ingress == "" {
				ingress = i.Hostname
			}
			ingressList = append(ingressList, ingress)
		}

		return fmt.Sprintf("📦 Updated: %s", strings.Join(ingressList, ", ")), err
	}

	for attempts := 0; ; attempts++ {
		event := <-watcher.ResultChan()
		service := event.Object.(*corev1.Service)
		if len(service.Status.LoadBalancer.Ingress) > 0 {
			for _, i := range service.Status.LoadBalancer.Ingress {
				ingress := i.IP
				if ingress == "" {
					ingress = i.Hostname
				}
				ingressList = append(ingressList, ingress)
			}
			return fmt.Sprintf("📦 Updated: %s", strings.Join(ingressList, ", ")), err
		}

		if attempts > serviceUpdateAttempts {
			return fmt.Sprintf("⛔️ Service update failed. Exceeded %d attempts", attempts), nil
		}
		log.Printf("📦 Waiting for load balancer assignment.")
	}
}
