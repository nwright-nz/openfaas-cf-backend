package handlers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	cfclient "github.com/nwright-nz/go-cfclient"
	"github.com/nwright-nz/openfaas-cf-backend/metrics"
	"github.com/nwright-nz/openfaas-cf-backend/requests"
	"github.com/nwright-nz/openfaas-cf-backend/types"
)

// MakeNewFunctionHandler creates a new function (service) inside the swarm network.
func MakeNewFunctionHandler(metricsOptions metrics.MetricOptions, c *cfclient.Client, maxRestarts uint64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := ioutil.ReadAll(r.Body)

		request := requests.CreateFunctionRequest{}
		err := json.Unmarshal(body, &request)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		//fmt.Println(request)

		// TODO: review why this was here... debugging?
		// w.WriteHeader(http.StatusNotImplemented)

		//nigel - need to re-enable this..
		//options := types.ServiceCreateOptions{}
		// if len(request.RegistryAuth) > 0 {
		// 	auth, err := BuildEncodedAuthConfig(request.RegistryAuth, request.Image)
		// 	if err != nil {
		// 		log.Println("Error while building registry auth configuration", err)
		// 		w.WriteHeader(http.StatusBadRequest)
		// 		w.Write([]byte("Invalid registry auth"))
		// 		return
		// 	}
		// 	options.EncodedRegistryAuth = auth
		// }

		//spec := makeSpec(&request, maxRestarts)
		//b := new(bytes.Buffer)
		// json.NewEncoder(b).Encode(spec)
		// fmt.Println(b)
		// fmt.Println("The spec is : " + spec.Command + spec.Dockerimage)

		org, _ := c.GetOrgByName("system")
		space, _ := c.GetSpaceByName("dev", org.Guid)

		var env = make(map[string]string)
		env["function"] = "true"
		env["fprocess"] = request.EnvProcess
		//env = fmt.Sprintf("{\"fprocess\": \"%s\", \"function\": \"true\"}", request.EnvProcess)
		//}
		for k, v := range request.EnvVars {
			env[k] = v
		}

		app, err := c.CreateV3DockerAppWithEnv(request.Service, space.Guid, env)
		if err != nil {
			fmt.Println(err)

		}

		pkg, err := c.CreateV3DockerPackage(app.GUID, request.Image)
		if err != nil {
			fmt.Println(err)

		}

		bld, err := c.CreateV3DockerBuild(pkg.GUID)
		if err != nil {
			fmt.Println(err)
		}

		dropletGUID := bld.Droplet.GUID

		for len(dropletGUID) == 0 {
			bldInfo, _ := c.GetV3BuildInfo(bld.GUID)
			dropletGUID = bldInfo.Droplet.GUID

			fmt.Println("Waiting for application to stage...")
			time.Sleep(2 * time.Second)
		}

		_, err = c.AssignDropletToApp(app.GUID, dropletGUID)
		if err != nil {
			fmt.Println("Error assigning droplet" + err.Error())
		}

		routeReq := cfclient.RouteRequest{Host: request.Service, DomainGuid: "5aca508e-e623-44d2-8b3e-8279788e1bcb", SpaceGuid: space.Guid}
		route, err := c.CreateHttpRoute(routeReq)
		if err != nil {
			fmt.Println(err)
		}

		routeMap := cfclient.RouteMap{AppGUID: app.GUID, RouteGUID: route.Meta.GUID}
		mr, err := c.MapRoute(routeMap)

		if err != nil {
			fmt.Println(err)
		}

		fmt.Println(mr)

		_, err = c.StartApp(app.GUID)

		if err != nil {
			fmt.Println("Error starting app " + err.Error())
		}

	}
}

func makeSpec(request *requests.CreateFunctionRequest, maxRestarts uint64) types.AppSpec {
	//Guardian doesnt do 'constraints' as such. Need to figure out the options here.
	// linuxOnlyConstraints := []string{"node.platform.os == linux"}
	// constraints := []string{}
	// if request.Constraints != nil && len(request.Constraints) > 0 {
	// 	constraints = request.Constraints
	// } else {
	// 	constraints = linuxOnlyConstraints
	// }

	spec := types.AppSpec{
		Dockerimage: request.Image,
		Name:        request.Service,
		Command:     "fwatchdog",
		Spaceguid:   "6888d55e-8ecf-4d05-9daa-5c788a8d2044",
		Instances:   1,
		//State:       "STARTED",
		Diego: true,
	}

	// TODO: request.EnvProcess should only be set if it's not nil, otherwise we override anything in the Docker image already
	//var env []string
	//if len(request.EnvProcess) > 0 {
	var env = make(map[string]string)
	env["function"] = "true"
	env["fprocess"] = request.EnvProcess
	//env = fmt.Sprintf("{\"fprocess\": \"%s\", \"function\": \"true\"}", request.EnvProcess)
	//}
	for k, v := range request.EnvVars {
		env[k] = v
	}

	// if len(env) > 0 {

	spec.Env = env
	//}

	return spec
}

// BuildEncodedAuthConfig for private registry
//nigel - dropping this out while i get my head around everything.

// func BuildEncodedAuthConfig(basicAuthB64 string, dockerImage string) (string, error) {
// 	// extract registry server address
// 	distributionRef, err := reference.ParseNormalizedNamed(dockerImage)
// 	if err != nil {
// 		return "", err
// 	}
// 	repoInfo, err := registry.ParseRepositoryInfo(distributionRef)
// 	if err != nil {
// 		return "", err
// 	}
// 	// extract registry user & password
// 	user, password, err := userPasswordFromBasicAuth(basicAuthB64)
// 	if err != nil {
// 		return "", err
// 	}
// 	// build encoded registry auth config
// 	buf, err := json.Marshal(types.AuthConfig{
// 		Username:      user,
// 		Password:      password,
// 		ServerAddress: repoInfo.Index.Name,
// 	})
// 	if err != nil {
// 		return "", err
// 	}
// 	return base64.URLEncoding.EncodeToString(buf), nil
// }

func userPasswordFromBasicAuth(basicAuthB64 string) (string, string, error) {
	c, err := base64.StdEncoding.DecodeString(basicAuthB64)
	if err != nil {
		return "", "", err
	}
	cs := string(c)
	s := strings.IndexByte(cs, ':')
	if s < 0 {
		return "", "", errors.New("Invalid basic auth")
	}
	return cs[:s], cs[s+1:], nil
}
