package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"sync"
	"time"

	"github.com/fnproject/fn_go/clientv2"
	"github.com/fnproject/fn_go/clientv2/apps"
	"github.com/fnproject/fn_go/clientv2/fns"
	models "github.com/fnproject/fn_go/modelsv2"
	openapi "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
	"gonum.org/v1/gonum/stat"
)

func main() {
	fmt.Println(time.Now().Format(time.StampMilli))
	n := flag.Int("n", 1, "number of invokes")
	p := flag.Int("p", 1, "parallel threads")
	appName := flag.String("app", "", "app name of function")
	fnName := flag.String("fn", "", "name of function")
	host := flag.String("host", "http://localhost:8080", "url of fn service")
	// TODO(reed): take body from stdin
	flag.Parse()

	invokes := *n
	threads := *p

	if threads < 1 {
		log.Fatal("p < 1")
	}
	if invokes < 1 {
		log.Fatal("n < 1")
	}
	if *appName == "" {
		log.Fatal("app name must be non-empty string")
	}
	if *fnName == "" {
		log.Fatal("fn name must be non-empty string")
	}

	// TODO(reed): this is awful, nobody should have to do this
	url, err := url.Parse(*host)
	if err != nil {
		log.Fatal(err)
	}

	transport := openapi.New(url.Host, path.Join(url.Path, clientv2.DefaultBasePath), []string{url.Scheme})
	client := clientv2.New(transport, strfmt.Default)

	appsResp, err := client.Apps.ListApps(&apps.ListAppsParams{
		Context: context.Background(),
		Name:    appName,
	})
	if err != nil {
		log.Fatal(err)
	}

	var app *models.App
	if len(appsResp.Payload.Items) > 0 {
		app = appsResp.Payload.Items[0]
	} else {
		log.Fatal("app not found")
	}

	resp, err := client.Fns.ListFns(&fns.ListFnsParams{
		Context: context.Background(),
		AppID:   &app.ID,
		Name:    fnName,
	})
	if err != nil {
		log.Fatal(err)
	}

	var fn *models.Fn
	for i := 0; i < len(resp.Payload.Items); i++ {
		if resp.Payload.Items[i].Name == *fnName {
			fn = resp.Payload.Items[i]
		}
	}
	if fn == nil {
		log.Fatal("fn not found")
	}

	httpClient := &http.Client{Transport: &http.Transport{
		Dial: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 1 * time.Minute,
		}).Dial,
		TLSClientConfig: &tls.Config{
			ClientSessionCache: tls.NewLRUClientSessionCache(8192),
		},
		TLSHandshakeTimeout:   10 * time.Second,
		MaxIdleConnsPerHost:   512,
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          512,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}}

	var wg sync.WaitGroup
	var plot points
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < invokes/threads; j++ {
				req, err := http.NewRequest("POST", *host+"/invoke/"+fn.ID, nil)
				if err != nil {
					log.Println(err)
				}
				// TODO(reed):
				req.Header.Set("Content-Type", "text/plain")

				start := time.Now()
				resp, err := httpClient.Do(req)
				if err != nil {
					log.Println(err)
				}
				end := time.Now()
				plot = append(plot, point{start, end})

				if resp.StatusCode != 200 {
					log.Printf("bad status code: %v", resp.StatusCode)
					io.Copy(os.Stderr, resp.Body)
				}
				io.Copy(ioutil.Discard, resp.Body)
			}
		}()
	}

	wg.Wait()
	fmt.Println(time.Now().Format(time.StampMilli))

	sort.Sort(plot)
	fmt.Println(plot)

	// weight out the last 'threads' number - this shaves off cold start/freeze
	var weights []float64
	if len(plot) > threads {
		plot = plot[:len(plot)-threads]
	}

	floats := plot.toFloats()
	mean := time.Duration(stat.Mean(floats, weights))
	_, stdf := stat.MeanStdDev(floats, weights)
	std := time.Duration(stdf)
	_, varif := stat.MeanVariance(floats, weights)
	variance := time.Duration(varif)

	median := plot[len(plot)/2].dur()

	fmt.Println("max:", plot[len(plot)-1].dur(), "min:", plot[0].dur(), "mean:", mean, "median:", median, "std:", std, "variance:", variance)
}

type point struct {
	start, end time.Time
}

func (p point) dur() time.Duration { return p.end.Sub(p.start) }

type points []point

func (p points) Len() int           { return len(p) }
func (p points) Less(i, j int) bool { return p[i].end.Sub(p[i].start) < p[j].end.Sub(p[j].start) }
func (p points) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func (p points) String() string {
	var b bytes.Buffer
	for i, pp := range p {
		fmt.Fprintln(&b, i, pp.end.Sub(pp.start), pp.start, pp.end)
	}
	return b.String()
}

// duration floats
func (p points) toFloats() []float64 {
	f := make([]float64, len(p))
	for i, pp := range p {
		f[i] = float64(pp.end.Sub(pp.start))
	}
	return f
}
