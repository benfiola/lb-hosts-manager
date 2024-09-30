package manager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// manager will synchronize a cluster's load balancer services with a local hosts file
type manager struct {
	hostsFile    string
	ignoreErrors bool
	key          string
	kubeconfig   string
	logger       *slog.Logger
	interval     time.Duration
}

// ManagerOpts are options used to construct a new [manager]
type Opts struct {
	IgnoreErrors bool
	Kubeconfig   string
	Logger       *slog.Logger
	Interval     uint
}

// Constructs a new [manager] with the provided [ManagerOpts]
func New(o *Opts) (*manager, error) {
	l := o.Logger
	if l == nil {
		l = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	}

	i := time.Duration(o.Interval)
	if i == 0 {
		i = time.Duration(1)
	}
	i *= time.Second

	return &manager{
		hostsFile:    "/etc/hosts",
		ignoreErrors: o.IgnoreErrors,
		key:          "# lb-hosts-manager",
		kubeconfig:   o.Kubeconfig,
		logger:       l,
		interval:     i,
	}, nil
}

// Handles an error.
// If [manager] configured to ignore errors, error is logged and 'nil' is returned.
// Otherwise, returns an error.
func (m *manager) handleError(err error) error {
	if err == nil {
		return nil
	}
	err = fmt.Errorf("error building k8s rest config: %w", err)
	if !m.ignoreErrors {
		return err
	}
	m.logger.Error(err.Error())
	return nil
}

// Creates a client with the kubeconfig stored within the [manager].
// Returns an error if this fails.
func (m *manager) createClient() (client.Client, error) {
	// get client config
	rc, err := clientcmd.BuildConfigFromFlags("", m.kubeconfig)
	if err != nil {
		return nil, err
	}

	// build client
	s := runtime.NewScheme()
	corev1.AddToScheme(s)
	c, err := client.New(rc, client.Options{Scheme: s})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// service is a simplified representation of an 'exportable' service (i.e., something capable of being added to a hosts file)
type service struct {
	dns string
	ip  string
}

// Gets all 'exportable' services from the cluster
// Returns an error if this fails
func (m *manager) getExportedServices(c client.Client) ([]service, error) {
	svcl := &corev1.ServiceList{}
	err := c.List(context.Background(), svcl, &client.ListOptions{})
	if err != nil {
		return nil, err
	}
	svcs := []service{}
	for _, svc := range svcl.Items {
		if svc.Spec.Type != "LoadBalancer" {
			// ignore non-load balancer services
			continue
		}
		if len(svc.Status.LoadBalancer.Ingress) == 0 {
			// ignore load balancer services without ip addresses
			continue
		}
		d := fmt.Sprintf("%s.%s.svc", svc.Name, svc.Namespace)
		i := svc.Status.LoadBalancer.Ingress[0].IP
		svc := service{
			dns: d,
			ip:  i,
		}
		svcs = append(svcs, svc)
	}
	return svcs, nil
}

// Processes a list of services and returns a mapping of ip -> []dns.
func (m *manager) processServices(svcs []service) map[string][]string {
	ipm := map[string]map[string]bool{}
	for _, svc := range svcs {
		dnsm, ok := ipm[svc.ip]
		if !ok {
			dnsm = map[string]bool{}
			ipm[svc.ip] = dnsm
		}
		dnsm[svc.dns] = true
	}
	svcm := map[string][]string{}
	for ip, dnsm := range ipm {
		dnss := []string{}
		for dns := range dnsm {
			dnss = append(dnss, dns)
		}
		svcm[ip] = dnss
	}
	return svcm
}

// Updates a hosts file with the provided ip -> []dns mapping.
// Returns an error if the update fails.
func (m *manager) updateHostsFile(svcm map[string][]string) error {
	// read hosts file
	hb, err := os.ReadFile(m.hostsFile)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		hb = []byte("")
	}
	h := string(hb)

	// update existing entries in host file
	ls := []string{}
	for _, l := range strings.Split(h, "\n") {
		lts := strings.TrimSpace(l)
		if !strings.HasSuffix(lts, m.key) {
			// line not managed by hosts-manager - keep
			ls = append(ls, l)
			continue
		}
		ps := strings.Split(lts, "\t")
		ip := ps[0]
		odnsstr := ps[1]
		ndnss, ok := svcm[ip]
		if !ok {
			// ip address no longer 'exported' - remove
			m.logger.Info(fmt.Sprintf("removing entry: %s", ip))
			continue
		}
		slices.Sort(ndnss)
		ndnsstr := strings.Join(ndnss, " ")
		delete(svcm, ip)
		if odnsstr == ndnsstr {
			// dns record unchanged - keep
			ls = append(ls, l)
			continue
		}
		//  update dns record
		m.logger.Info(fmt.Sprintf("updating entry: %s (%s)", ip, ndnsstr))
		nl := fmt.Sprintf("%s\t%s\t%s", ip, ndnsstr, m.key)
		ls = append(ls, nl)
	}

	// add new entries to host file
	for ip, dnss := range svcm {
		slices.Sort(dnss)
		dnsstr := strings.Join(dnss, " ")
		m.logger.Info(fmt.Sprintf("adding entry: %s (%s)", ip, dnsstr))
		nl := fmt.Sprintf("%s\t%s\t%s", ip, dnsstr, m.key)
		ls = append(ls, nl)
	}

	// write new hosts file
	d := []byte(strings.Join(ls, "\n"))
	err = os.WriteFile(m.hostsFile, d, 0644)
	if err != nil {
		return err
	}
	return nil
}

// Performs a single tick of the [manager] run loop.
// Creates a client.
// Gets 'exported' services.
// Syncs services wtih hosts file.
func (m *manager) tick() error {
	c, err := m.createClient()
	if err != nil {
		return err
	}
	svcs, err := m.getExportedServices(c)
	if err != nil {
		return err
	}
	svcm := m.processServices(svcs)
	err = m.updateHostsFile(svcm)
	if err != nil {
		return err
	}
	return nil
}

// Executes the [manager] run loop until the provided [context.Context] is cancelled.
func (m *manager) loop(ctx context.Context) error {
	m.logger.Info("starting manager")

	m.logger.Info(fmt.Sprintf("kubeconfig: %s", m.kubeconfig))

	for {
		// handle cancellation
		cncl := false
		select {
		case <-ctx.Done():
			cncl = true
		default:
		}
		if cncl {
			m.logger.Info("manager received cancellation request")
			break
		}

		err := m.tick()
		if err = m.handleError(err); err != nil {
			return err
		}

		time.Sleep(m.interval)
	}

	m.logger.Info("stopped")
	return nil
}

// Wraps the [manager] run loop with signal handling and cancellation
func (m *manager) Run(ctx context.Context) error {
	// establish signal handlers
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM)

	// start runloop within wait group, with cancellable context
	var wg sync.WaitGroup
	sctx, cancel := context.WithCancel(ctx)
	var err error
	wg.Add(1)
	go func(ctx context.Context) {
		defer wg.Done()
		err = m.loop(ctx)
	}(sctx)

	// handle signal when received
	s := <-sc
	m.logger.Info(fmt.Sprintf("signal '%v' received - cancelling", s))
	cancel()
	wg.Wait()

	if err != nil {
		if !errors.Is(err, context.Canceled) {
			// ignore non-cancellation errors
			return err
		}
	}

	return nil
}
