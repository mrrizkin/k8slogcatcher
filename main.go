package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Config struct {
	KubeConfig    string
	Namespace     string
	InCluster     bool
	LogRetention  time.Duration
	BufferSize    int
	FlushInterval time.Duration
}

type LogWriter struct {
	pod           *corev1.Pod
	logDir        string
	currentFile   *os.File
	currentDate   string
	buffer        []byte
	bufferSize    int
	flushInterval time.Duration
	mu            sync.Mutex
}

func main() {
	config := parseFlags()

	clientset, err := createClientset(config)
	if err != nil {
		log.Fatalf("Error creating Kubernetes clientset: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	monitorPods(ctx, &wg, clientset, config)

	handleSignals(cancel)
	wg.Wait()
	log.Println("Log catcher shutting down")
}

func parseFlags() *Config {
	config := &Config{}
	flag.StringVar(&config.KubeConfig, "kubeconfig", "", "Path to kubeconfig file")
	flag.StringVar(&config.Namespace, "namespace", "", "Namespace to monitor (empty for all)")
	flag.BoolVar(&config.InCluster, "in-cluster", false, "Use in-cluster configuration")
	flag.DurationVar(&config.LogRetention, "retention", 7*24*time.Hour, "Log retention period")
	flag.IntVar(&config.BufferSize, "buffer-size", 1024*1024, "Buffer size for log writing")
	flag.DurationVar(
		&config.FlushInterval,
		"flush-interval",
		5*time.Second,
		"Interval for flushing logs to disk",
	)
	flag.Parse()
	return config
}

func createClientset(config *Config) (*kubernetes.Clientset, error) {
	var k8sConfig *rest.Config
	var err error

	if config.InCluster {
		k8sConfig, err = rest.InClusterConfig()
	} else {
		if config.KubeConfig == "" {
			config.KubeConfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
		}
		k8sConfig, err = clientcmd.BuildConfigFromFlags("", config.KubeConfig)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create K8s config: %v", err)
	}

	return kubernetes.NewForConfig(k8sConfig)
}

func monitorPods(
	ctx context.Context,
	wg *sync.WaitGroup,
	clientset *kubernetes.Clientset,
	config *Config,
) {
	watch, err := clientset.CoreV1().Pods(config.Namespace).Watch(ctx, metav1.ListOptions{})
	if err != nil {
		log.Fatalf("Error watching pods: %v", err)
	}

	for event := range watch.ResultChan() {
		pod, ok := event.Object.(*corev1.Pod)
		if !ok {
			log.Printf("Unexpected object type: %v", event.Object)
			continue
		}

		switch event.Type {
		case "ADDED", "MODIFIED":
			wg.Add(1)
			go func(p *corev1.Pod) {
				defer wg.Done()
				streamPodLogs(ctx, clientset, p, config)
			}(pod)
		case "DELETED":
			log.Printf("Pod %s deleted", pod.Name)
		}
	}
}

func streamPodLogs(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	pod *corev1.Pod,
	config *Config,
) {
	req := clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Follow: true,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		log.Printf("Error opening log stream for pod %s: %v", pod.Name, err)
		return
	}
	defer stream.Close()

	deploymentName := getDeploymentName(pod)
	logDir := filepath.Join("logs", pod.Namespace, deploymentName)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Printf("Error creating directory for pod %s: %v", pod.Name, err)
		return
	}

	logWriter := &LogWriter{
		pod:           pod,
		logDir:        logDir,
		bufferSize:    config.BufferSize,
		flushInterval: config.FlushInterval,
		buffer:        make([]byte, 0, config.BufferSize),
	}
	defer logWriter.close()

	go logWriter.periodicFlush(ctx)

	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
			logWriter.write(scanner.Bytes())
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading log stream for pod %s: %v", pod.Name, err)
	}
}

func (lw *LogWriter) write(line []byte) {
	lw.mu.Lock()
	defer lw.mu.Unlock()

	now := time.Now()
	dateStr := now.Format("2006-01-02")

	if dateStr != lw.currentDate {
		lw.rotateFile(dateStr)
	}

	logLine := fmt.Sprintf("[%s] %s\n", now.Format("15:04:05"), line)
	lw.buffer = append(lw.buffer, []byte(logLine)...)

	if len(lw.buffer) >= lw.bufferSize {
		lw.flush()
	}
}

func (lw *LogWriter) rotateFile(dateStr string) {
	if lw.currentFile != nil {
		lw.flush()
		lw.currentFile.Close()
	}

	logFileName := filepath.Join(lw.logDir, fmt.Sprintf("%s_%s.log", lw.pod.Name, dateStr))
	newLogFile, err := os.OpenFile(logFileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error opening new log file for pod %s: %v", lw.pod.Name, err)
		return
	}

	lw.currentFile = newLogFile
	lw.currentDate = dateStr
}

func (lw *LogWriter) flush() {
	if lw.currentFile != nil && len(lw.buffer) > 0 {
		_, err := lw.currentFile.Write(lw.buffer)
		if err != nil {
			log.Printf("Error writing to log file for pod %s: %v", lw.pod.Name, err)
		}
		lw.buffer = lw.buffer[:0]
	}
}

func (lw *LogWriter) periodicFlush(ctx context.Context) {
	ticker := time.NewTicker(lw.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lw.mu.Lock()
			lw.flush()
			lw.mu.Unlock()
		}
	}
}

func (lw *LogWriter) close() {
	lw.mu.Lock()
	defer lw.mu.Unlock()

	lw.flush()
	if lw.currentFile != nil {
		lw.currentFile.Close()
	}
}

func getDeploymentName(pod *corev1.Pod) string {
	if deploymentName, ok := pod.Labels["app"]; ok {
		return deploymentName
	}
	return pod.Spec.Containers[0].Name
}

func handleSignals(cancel context.CancelFunc) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Received shutdown signal")
		cancel()
	}()
}
