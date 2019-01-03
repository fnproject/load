package main

import (
	fnclient "github.com/fnproject/fn_go/clientv2"
	models "github.com/fnproject/fn_go/modelsv2"
	openapi "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
)

func main() {
	n := flag.Int("n", 1, "number of invokes")
	p := flag.Int("p", 1, "parallel threads")
	appName := flag.String("app", "", "app name of function")
	fnName := flag.String("fn", "", "name of function")
	host := flag.String("host", "http://localhost:8080", "url of fn service")
	// TODO(reed): take body from stdin

	invokes := *n
	threads := *p

	if threads < 1 {
		log.Fatal("p < 1")
	}
	if invokes < 1 {
		log.Fatal("n < 1")
	}

	// TODO(reed): this is awful, nobody should have to do this
	transport := openapi.New(*host, path.Join("/", clientv2.DefaultBasePath), "http")
	client := clientv2.New(transport, strfmt.Default)

	appsResp, err := client.Apps.ListApps(&apiapps.ListAppsParams{
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

	resp, err := client.Fns.ListFns(&apifns.ListFnsParams{
		Context: context.Background(),
		AppID:   app.ID,
		Name:    *fn,
	})
	if err != nil {
		log.Fatal(err)
	}

	var fn *models.Fn
	for i := 0; i < len(resp.Payload.Items); i++ {
		if resp.Payload.Items[i].Name == fnName {
			fn = resp.Payload.Items[i]
		}
	}
	if fn == nil {
		log.Fatal("fn not found")
	}

	var wg sync.WaitGroup
	for i := 0; i < threads; i++ {
		go func() {
			wg.Add(1)
			defer wg.Done()
			for j := 0; j < invokes/threads; j++ {
				req, err := http.NewRequest("POST", host+"/invoke/"+fn.ID, nil)
				if err != nil {
					log.Error(err)
				}
				// TODO(reed):
				req.Header.Set("Content-Type", "text/plain")

				resp, err := httpClient.Do(req)
				if err != nil {
					log.Error(err)
				}
				if resp.StatusCode != 200 {
					log.Errorf("bad status code: %v", resp.StatusCode)
					io.Copy(os.Stderr, resp.Body)
				}
				io.Copy(os.Stdout, resp.Body)
			}
		}()
	}

	wg.Wait()
}
