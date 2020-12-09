package main

import (
	"burghardt.tech/shadowController/pkg/generated/clientset/versioned"
	"context"
	"flag"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

func main() {
	kubeconfig := getConfig()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	clientset, err := versioned.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	c := make(chan os.Signal)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for {
			listShadow(clientset)
			time.Sleep(time.Second* 3)
		}
	}()

	<-c
}

func listShadow(clientset *versioned.Clientset) {
	list, err := clientset.BurghardtV1().Shadows("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.Fatalf("Error listing all databases: %v", err)
	}
	for _, sh := range list.Items {
		fmt.Println("Shadow resource named: ", sh.Spec.PodName)
	}
}

func getConfig() *string {
	var kubeconfig *string
	if home := homeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()
	return kubeconfig
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE")
}
