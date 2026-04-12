package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	json "github.com/goccy/go-json"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/errdefs"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

type mockServiceLifecycleWriter struct {
	scaleServiceFn              func(ctx context.Context, id string, replicas uint64) (swarm.Service, error)
	updateServiceImageFn        func(ctx context.Context, id string, image string) (swarm.Service, error)
	rollbackServiceFn           func(ctx context.Context, id string) (swarm.Service, error)
	restartServiceFn            func(ctx context.Context, id string) (swarm.Service, error)
	removeServiceFn             func(ctx context.Context, id string) error
	updateServiceModeFn         func(ctx context.Context, id string, mode swarm.ServiceMode) (swarm.Service, error)
	updateServiceEndpointModeFn func(ctx context.Context, id string, mode swarm.ResolutionMode) (swarm.Service, error)
}

type mockServiceSpecWriter struct {
	updateServiceEnvFn            func(ctx context.Context, id string, env map[string]string) (swarm.Service, error)
	updateServiceLabelsFn         func(ctx context.Context, id string, labels map[string]string) (swarm.Service, error)
	updateServiceResourcesFn      func(ctx context.Context, id string, resources *swarm.ResourceRequirements) (swarm.Service, error)
	updateServiceHealthcheckFn    func(ctx context.Context, id string, hc *container.HealthConfig) (swarm.Service, error)
	updateServicePlacementFn      func(ctx context.Context, id string, placement *swarm.Placement) (swarm.Service, error)
	updateServicePortsFn          func(ctx context.Context, id string, ports []swarm.PortConfig) (swarm.Service, error)
	updateServiceUpdatePolicyFn   func(ctx context.Context, id string, policy *swarm.UpdateConfig) (swarm.Service, error)
	updateServiceRollbackPolicyFn func(ctx context.Context, id string, policy *swarm.UpdateConfig) (swarm.Service, error)
	updateServiceLogDriverFn      func(ctx context.Context, id string, driver *swarm.Driver) (swarm.Service, error)
}

type mockServiceAttachmentWriter struct {
	updateServiceContainerConfigFn func(ctx context.Context, id string, apply func(spec *swarm.ContainerSpec)) (swarm.Service, error)
	updateServiceConfigsFn         func(ctx context.Context, id string, configs []*swarm.ConfigReference) (swarm.Service, error)
	updateServiceSecretsFn         func(ctx context.Context, id string, secrets []*swarm.SecretReference) (swarm.Service, error)
	updateServiceNetworksFn        func(ctx context.Context, id string, networks []swarm.NetworkAttachmentConfig) (swarm.Service, error)
	updateServiceMountsFn          func(ctx context.Context, id string, mounts []mount.Mount) (swarm.Service, error)
}

type mockNodeWriter struct {
	updateNodeAvailabilityFn func(ctx context.Context, id string, availability swarm.NodeAvailability) (swarm.Node, error)
	updateNodeLabelsFn       func(ctx context.Context, id string, labels map[string]string) (swarm.Node, error)
	updateNodeRoleFn         func(ctx context.Context, id string, role swarm.NodeRole) (swarm.Node, error)
	removeNodeFn             func(ctx context.Context, id string, force bool) error
}

type mockConfigWriter struct {
	createConfigFn       func(ctx context.Context, spec swarm.ConfigSpec) (string, error)
	removeConfigFn       func(ctx context.Context, id string) error
	updateConfigLabelsFn func(ctx context.Context, id string, labels map[string]string) (swarm.Config, error)
}

type mockSecretWriter struct {
	createSecretFn       func(ctx context.Context, spec swarm.SecretSpec) (string, error)
	removeSecretFn       func(ctx context.Context, id string) error
	updateSecretLabelsFn func(ctx context.Context, id string, labels map[string]string) (swarm.Secret, error)
}

type mockResourceRemover struct {
	removeTaskFn    func(ctx context.Context, id string) error
	removeNetworkFn func(ctx context.Context, id string) error
	removeVolumeFn  func(ctx context.Context, name string, force bool) error
}

type mockWriteClient struct {
	mockServiceLifecycleWriter
	mockServiceSpecWriter
	mockServiceAttachmentWriter
	mockNodeWriter
	mockConfigWriter
	mockSecretWriter
	mockResourceRemover
}

func (m *mockServiceLifecycleWriter) ScaleService(
	ctx context.Context,
	id string,
	replicas uint64,
) (swarm.Service, error) {
	if m.scaleServiceFn != nil {
		return m.scaleServiceFn(ctx, id, replicas)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockServiceLifecycleWriter) UpdateServiceImage(
	ctx context.Context,
	id string,
	image string,
) (swarm.Service, error) {
	if m.updateServiceImageFn != nil {
		return m.updateServiceImageFn(ctx, id, image)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockServiceLifecycleWriter) RollbackService(
	ctx context.Context,
	id string,
) (swarm.Service, error) {
	if m.rollbackServiceFn != nil {
		return m.rollbackServiceFn(ctx, id)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockServiceLifecycleWriter) RestartService(
	ctx context.Context,
	id string,
) (swarm.Service, error) {
	if m.restartServiceFn != nil {
		return m.restartServiceFn(ctx, id)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockNodeWriter) UpdateNodeAvailability(
	ctx context.Context,
	id string,
	availability swarm.NodeAvailability,
) (swarm.Node, error) {
	if m.updateNodeAvailabilityFn != nil {
		return m.updateNodeAvailabilityFn(ctx, id, availability)
	}
	return swarm.Node{}, fmt.Errorf("not implemented")
}

func (m *mockResourceRemover) RemoveTask(ctx context.Context, id string) error {
	if m.removeTaskFn != nil {
		return m.removeTaskFn(ctx, id)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockServiceSpecWriter) UpdateServiceEnv(
	ctx context.Context,
	id string,
	env map[string]string,
) (swarm.Service, error) {
	if m.updateServiceEnvFn != nil {
		return m.updateServiceEnvFn(ctx, id, env)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockNodeWriter) UpdateNodeLabels(
	ctx context.Context,
	id string,
	labels map[string]string,
) (swarm.Node, error) {
	if m.updateNodeLabelsFn != nil {
		return m.updateNodeLabelsFn(ctx, id, labels)
	}
	return swarm.Node{}, fmt.Errorf("not implemented")
}

func (m *mockNodeWriter) UpdateNodeRole(
	ctx context.Context,
	id string,
	role swarm.NodeRole,
) (swarm.Node, error) {
	if m.updateNodeRoleFn != nil {
		return m.updateNodeRoleFn(ctx, id, role)
	}
	return swarm.Node{}, fmt.Errorf("not implemented")
}

func (m *mockNodeWriter) RemoveNode(ctx context.Context, id string, force bool) error {
	if m.removeNodeFn != nil {
		return m.removeNodeFn(ctx, id, force)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockResourceRemover) RemoveNetwork(ctx context.Context, id string) error {
	if m.removeNetworkFn != nil {
		return m.removeNetworkFn(ctx, id)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockConfigWriter) RemoveConfig(ctx context.Context, id string) error {
	if m.removeConfigFn != nil {
		return m.removeConfigFn(ctx, id)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockSecretWriter) RemoveSecret(ctx context.Context, id string) error {
	if m.removeSecretFn != nil {
		return m.removeSecretFn(ctx, id)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockConfigWriter) CreateConfig(
	ctx context.Context,
	spec swarm.ConfigSpec,
) (string, error) {
	if m.createConfigFn != nil {
		return m.createConfigFn(ctx, spec)
	}
	return "", fmt.Errorf("not implemented")
}

func (m *mockSecretWriter) CreateSecret(
	ctx context.Context,
	spec swarm.SecretSpec,
) (string, error) {
	if m.createSecretFn != nil {
		return m.createSecretFn(ctx, spec)
	}
	return "", fmt.Errorf("not implemented")
}

func (m *mockConfigWriter) UpdateConfigLabels(
	ctx context.Context,
	id string,
	labels map[string]string,
) (swarm.Config, error) {
	if m.updateConfigLabelsFn != nil {
		return m.updateConfigLabelsFn(ctx, id, labels)
	}
	return swarm.Config{}, fmt.Errorf("not implemented")
}

func (m *mockSecretWriter) UpdateSecretLabels(
	ctx context.Context,
	id string,
	labels map[string]string,
) (swarm.Secret, error) {
	if m.updateSecretLabelsFn != nil {
		return m.updateSecretLabelsFn(ctx, id, labels)
	}
	return swarm.Secret{}, fmt.Errorf("not implemented")
}

func (m *mockResourceRemover) RemoveVolume(ctx context.Context, name string, force bool) error {
	if m.removeVolumeFn != nil {
		return m.removeVolumeFn(ctx, name, force)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockServiceSpecWriter) UpdateServiceLabels(
	ctx context.Context,
	id string,
	labels map[string]string,
) (swarm.Service, error) {
	if m.updateServiceLabelsFn != nil {
		return m.updateServiceLabelsFn(ctx, id, labels)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockServiceSpecWriter) UpdateServiceResources(
	ctx context.Context,
	id string,
	resources *swarm.ResourceRequirements,
) (swarm.Service, error) {
	if m.updateServiceResourcesFn != nil {
		return m.updateServiceResourcesFn(ctx, id, resources)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockServiceLifecycleWriter) UpdateServiceEndpointMode(
	ctx context.Context,
	id string,
	mode swarm.ResolutionMode,
) (swarm.Service, error) {
	if m.updateServiceEndpointModeFn != nil {
		return m.updateServiceEndpointModeFn(ctx, id, mode)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockServiceLifecycleWriter) UpdateServiceMode(
	ctx context.Context,
	id string,
	mode swarm.ServiceMode,
) (swarm.Service, error) {
	if m.updateServiceModeFn != nil {
		return m.updateServiceModeFn(ctx, id, mode)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockServiceSpecWriter) UpdateServiceHealthcheck(
	ctx context.Context,
	id string,
	hc *container.HealthConfig,
) (swarm.Service, error) {
	if m.updateServiceHealthcheckFn != nil {
		return m.updateServiceHealthcheckFn(ctx, id, hc)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockServiceSpecWriter) UpdateServicePlacement(
	ctx context.Context,
	id string,
	placement *swarm.Placement,
) (swarm.Service, error) {
	if m.updateServicePlacementFn != nil {
		return m.updateServicePlacementFn(ctx, id, placement)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockServiceSpecWriter) UpdateServicePorts(
	ctx context.Context,
	id string,
	ports []swarm.PortConfig,
) (swarm.Service, error) {
	if m.updateServicePortsFn != nil {
		return m.updateServicePortsFn(ctx, id, ports)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockServiceSpecWriter) UpdateServiceUpdatePolicy(
	ctx context.Context,
	id string,
	policy *swarm.UpdateConfig,
) (swarm.Service, error) {
	if m.updateServiceUpdatePolicyFn != nil {
		return m.updateServiceUpdatePolicyFn(ctx, id, policy)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockServiceSpecWriter) UpdateServiceRollbackPolicy(
	ctx context.Context,
	id string,
	policy *swarm.UpdateConfig,
) (swarm.Service, error) {
	if m.updateServiceRollbackPolicyFn != nil {
		return m.updateServiceRollbackPolicyFn(ctx, id, policy)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockServiceSpecWriter) UpdateServiceLogDriver(
	ctx context.Context,
	id string,
	driver *swarm.Driver,
) (swarm.Service, error) {
	if m.updateServiceLogDriverFn != nil {
		return m.updateServiceLogDriverFn(ctx, id, driver)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockServiceAttachmentWriter) UpdateServiceContainerConfig(
	ctx context.Context,
	id string,
	apply func(spec *swarm.ContainerSpec),
) (swarm.Service, error) {
	if m.updateServiceContainerConfigFn != nil {
		return m.updateServiceContainerConfigFn(ctx, id, apply)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockServiceAttachmentWriter) UpdateServiceConfigs(
	ctx context.Context,
	id string,
	configs []*swarm.ConfigReference,
) (swarm.Service, error) {
	if m.updateServiceConfigsFn != nil {
		return m.updateServiceConfigsFn(ctx, id, configs)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockServiceAttachmentWriter) UpdateServiceSecrets(
	ctx context.Context,
	id string,
	secrets []*swarm.SecretReference,
) (swarm.Service, error) {
	if m.updateServiceSecretsFn != nil {
		return m.updateServiceSecretsFn(ctx, id, secrets)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockServiceAttachmentWriter) UpdateServiceNetworks(
	ctx context.Context,
	id string,
	networks []swarm.NetworkAttachmentConfig,
) (swarm.Service, error) {
	if m.updateServiceNetworksFn != nil {
		return m.updateServiceNetworksFn(ctx, id, networks)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockServiceAttachmentWriter) UpdateServiceMounts(
	ctx context.Context,
	id string,
	mounts []mount.Mount,
) (swarm.Service, error) {
	if m.updateServiceMountsFn != nil {
		return m.updateServiceMountsFn(ctx, id, mounts)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockServiceLifecycleWriter) RemoveService(ctx context.Context, id string) error {
	if m.removeServiceFn != nil {
		return m.removeServiceFn(ctx, id)
	}
	return fmt.Errorf("not implemented")
}

func replicatedService(id string) swarm.Service {
	replicas := uint64(1)
	return swarm.Service{
		ID: id,
		Spec: swarm.ServiceSpec{
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
		},
	}
}

func TestHandleScaleService_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{
		mockServiceLifecycleWriter: mockServiceLifecycleWriter{
			scaleServiceFn: func(_ context.Context, id string, replicas uint64) (swarm.Service, error) {
				svc := replicatedService(id)
				svc.Spec.Mode.Replicated.Replicas = &replicas
				return svc, nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"replicas":3}`
	req := httptest.NewRequest("PUT", "/services/svc1/scale", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleScaleService(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "Service" {
		t.Errorf("@type=%v, want Service", resp["@type"])
	}
}

func TestHandleScaleService_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"replicas":2}`
	req := httptest.NewRequest("PUT", "/services/missing/scale", strings.NewReader(body))
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleScaleService(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleScaleService_GlobalMode(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svcglobal",
		Spec: swarm.ServiceSpec{
			Mode: swarm.ServiceMode{
				Global: &swarm.GlobalService{},
			},
		},
	})
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"replicas":2}`
	req := httptest.NewRequest("PUT", "/services/svcglobal/scale", strings.NewReader(body))
	req.SetPathValue("id", "svcglobal")
	w := httptest.NewRecorder()
	h.HandleScaleService(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleScaleService_Conflict(t *testing.T) {
	c := cache.New(nil)
	replicas := uint64(3)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
		},
	})

	wc := &mockWriteClient{
		mockServiceLifecycleWriter: mockServiceLifecycleWriter{
			scaleServiceFn: func(_ context.Context, _ string, _ uint64) (swarm.Service, error) {
				return swarm.Service{}, errdefs.Conflict(fmt.Errorf("update out of sequence"))
			},
		},
	}

	h := newTestHandlers(t, withCache(c), withWriteClient(wc))
	body := `{"replicas": 5}`
	req := httptest.NewRequest("PUT", "/services/svc1/scale", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleScaleService(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status=%d, want 409", w.Code)
	}
}

func TestHandleScaleService_InvalidBody(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("PUT", "/services/svc1/scale", strings.NewReader("not json"))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleScaleService(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleUpdateServiceMode_ToGlobal(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{
		mockServiceLifecycleWriter: mockServiceLifecycleWriter{
			updateServiceModeFn: func(_ context.Context, id string, mode swarm.ServiceMode) (swarm.Service, error) {
				return swarm.Service{
					ID:   id,
					Spec: swarm.ServiceSpec{Mode: mode},
				}, nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"mode":"global"}`
	req := httptest.NewRequest("PUT", "/services/svc1/mode", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceMode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateServiceMode_ToReplicated(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Mode: swarm.ServiceMode{Global: &swarm.GlobalService{}}},
	})

	wc := &mockWriteClient{
		mockServiceLifecycleWriter: mockServiceLifecycleWriter{
			updateServiceModeFn: func(_ context.Context, id string, mode swarm.ServiceMode) (swarm.Service, error) {
				return swarm.Service{
					ID:   id,
					Spec: swarm.ServiceSpec{Mode: mode},
				}, nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"mode":"replicated","replicas":3}`
	req := httptest.NewRequest("PUT", "/services/svc1/mode", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceMode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateServiceMode_ReplicatedWithoutCount(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Mode: swarm.ServiceMode{Global: &swarm.GlobalService{}}},
	})

	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"mode":"replicated"}`
	req := httptest.NewRequest("PUT", "/services/svc1/mode", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceMode(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleUpdateServiceMode_InvalidMode(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"mode":"invalid"}`
	req := httptest.NewRequest("PUT", "/services/svc1/mode", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceMode(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleUpdateServiceMode_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"mode":"global"}`
	req := httptest.NewRequest("PUT", "/services/missing/mode", strings.NewReader(body))
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceMode(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleUpdateServiceEndpointMode_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{
		mockServiceLifecycleWriter: mockServiceLifecycleWriter{
			updateServiceEndpointModeFn: func(_ context.Context, id string, mode swarm.ResolutionMode) (swarm.Service, error) {
				svc := replicatedService(id)
				svc.Spec.EndpointSpec = &swarm.EndpointSpec{Mode: mode}
				return svc, nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"mode":"dnsrr"}`
	req := httptest.NewRequest("PUT", "/services/svc1/endpoint-mode", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceEndpointMode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateServiceEndpointMode_InvalidMode(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"mode":"invalid"}`
	req := httptest.NewRequest("PUT", "/services/svc1/endpoint-mode", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceEndpointMode(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleUpdateServiceImage_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{
		mockServiceLifecycleWriter: mockServiceLifecycleWriter{
			updateServiceImageFn: func(_ context.Context, id string, image string) (swarm.Service, error) {
				svc := replicatedService(id)
				svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{Image: image}
				return svc, nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"image":"nginx:latest"}`
	req := httptest.NewRequest("PUT", "/services/svc1/image", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceImage(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "Service" {
		t.Errorf("@type=%v, want Service", resp["@type"])
	}
}

func TestHandleUpdateServiceImage_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"image":"nginx:latest"}`
	req := httptest.NewRequest("PUT", "/services/missing/image", strings.NewReader(body))
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceImage(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleUpdateServiceImage_EmptyImage(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"image":""}`
	req := httptest.NewRequest("PUT", "/services/svc1/image", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceImage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func serviceWithPreviousSpec(id string) swarm.Service {
	spec := swarm.ServiceSpec{}
	svc := replicatedService(id)
	svc.PreviousSpec = &spec
	return svc
}

func TestHandleRollbackService_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(serviceWithPreviousSpec("svc1"))

	wc := &mockWriteClient{
		mockServiceLifecycleWriter: mockServiceLifecycleWriter{
			rollbackServiceFn: func(_ context.Context, id string) (swarm.Service, error) {
				return replicatedService(id), nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("POST", "/services/svc1/rollback", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleRollbackService(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "Service" {
		t.Errorf("@type=%v, want Service", resp["@type"])
	}
}

func TestHandleRollbackService_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("POST", "/services/missing/rollback", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleRollbackService(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleRollbackService_NoPreviousSpec(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("POST", "/services/svc1/rollback", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleRollbackService(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleRestartService_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{
		mockServiceLifecycleWriter: mockServiceLifecycleWriter{
			restartServiceFn: func(_ context.Context, id string) (swarm.Service, error) {
				return replicatedService(id), nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("POST", "/services/svc1/restart", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleRestartService(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "Service" {
		t.Errorf("@type=%v, want Service", resp["@type"])
	}
}

func TestHandleRestartService_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("POST", "/services/missing/restart", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleRestartService(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleUpdateNodeAvailability_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node1"})

	wc := &mockWriteClient{
		mockNodeWriter: mockNodeWriter{
			updateNodeAvailabilityFn: func(_ context.Context, id string, availability swarm.NodeAvailability) (swarm.Node, error) {
				return swarm.Node{ID: id, Spec: swarm.NodeSpec{Availability: availability}}, nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"availability":"drain"}`
	req := httptest.NewRequest("PUT", "/nodes/node1/availability", strings.NewReader(body))
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleUpdateNodeAvailability(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "Node" {
		t.Errorf("@type=%v, want Node", resp["@type"])
	}
}

func TestHandleUpdateNodeAvailability_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"availability":"drain"}`
	req := httptest.NewRequest("PUT", "/nodes/missing/availability", strings.NewReader(body))
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleUpdateNodeAvailability(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleUpdateNodeAvailability_InvalidAvailability(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node1"})
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"availability":"invalid"}`
	req := httptest.NewRequest("PUT", "/nodes/node1/availability", strings.NewReader(body))
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleUpdateNodeAvailability(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleRemoveTask_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(swarm.Task{ID: "task1"})

	wc := &mockWriteClient{
		mockResourceRemover: mockResourceRemover{
			removeTaskFn: func(_ context.Context, id string) error {
				return nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/tasks/task1", nil)
	req.SetPathValue("id", "task1")
	w := httptest.NewRecorder()
	h.HandleRemoveTask(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleRemoveTask_NotFoundInCache(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/tasks/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleRemoveTask(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleRemoveTask_NoContainer(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(swarm.Task{ID: "task1"})

	wc := &mockWriteClient{
		mockResourceRemover: mockResourceRemover{
			removeTaskFn: func(_ context.Context, id string) error {
				return errdefs.NotFound(fmt.Errorf("task has no running container"))
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/tasks/task1", nil)
	req.SetPathValue("id", "task1")
	w := httptest.NewRecorder()
	h.HandleRemoveTask(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleRemoveService_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{
		mockServiceLifecycleWriter: mockServiceLifecycleWriter{
			removeServiceFn: func(_ context.Context, id string) error {
				return nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/services/svc1", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleRemoveService(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleRemoveService_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/services/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleRemoveService(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleRemoveService_DockerError(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{
		mockServiceLifecycleWriter: mockServiceLifecycleWriter{
			removeServiceFn: func(_ context.Context, id string) error {
				return fmt.Errorf("engine error")
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/services/svc1", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleRemoveService(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", w.Code)
	}
}

func serviceWithEnv(id string, env []string) swarm.Service {
	svc := replicatedService(id)
	svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{Env: env}
	return svc
}

func TestHandleGetServiceEnv(t *testing.T) {
	c := cache.New(nil)
	c.SetService(serviceWithEnv("svc1", []string{"FOO=bar", "BAZ=qux"}))
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/services/svc1/env", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceEnv(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "ServiceEnv" {
		t.Errorf("@type=%v, want ServiceEnv", resp["@type"])
	}
	envMap, ok := resp["env"].(map[string]any)
	if !ok {
		t.Fatal("expected env key in response")
	}
	if envMap["FOO"] != "bar" {
		t.Errorf("FOO=%v, want bar", envMap["FOO"])
	}
	if envMap["BAZ"] != "qux" {
		t.Errorf("BAZ=%v, want qux", envMap["BAZ"])
	}
}

func TestHandlePatchServiceEnv_Add(t *testing.T) {
	c := cache.New(nil)
	c.SetService(serviceWithEnv("svc1", []string{"FOO=bar"}))

	wc := &mockWriteClient{
		mockServiceSpecWriter: mockServiceSpecWriter{
			updateServiceEnvFn: func(_ context.Context, id string, env map[string]string) (swarm.Service, error) {
				envSlice := make([]string, 0, len(env))
				for k, v := range env {
					envSlice = append(envSlice, k+"="+v)
				}
				return serviceWithEnv(id, envSlice), nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `[{"op":"add","path":"/NEW","value":"val"}]`
	req := httptest.NewRequest("PATCH", "/services/svc1/env", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceEnv(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "ServiceEnv" {
		t.Errorf("@type=%v, want ServiceEnv", resp["@type"])
	}
	if id, ok := resp["@id"].(string); !ok || !strings.HasSuffix(id, "/services/svc1/env") {
		t.Errorf("expected @id ending in /services/svc1/env, got %v", resp["@id"])
	}
	if ctx, ok := resp["@context"].(string); !ok || !strings.HasSuffix(ctx, "/api/context.jsonld") {
		t.Errorf("expected @context ending in /api/context.jsonld, got %v", resp["@context"])
	}
}

func TestHandlePatchServiceEnv_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetService(serviceWithEnv("svc1", nil))
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("PATCH", "/services/svc1/env", strings.NewReader(`[]`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceEnv(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}

func TestHandlePatchServiceEnv_TestFailed(t *testing.T) {
	c := cache.New(nil)
	c.SetService(serviceWithEnv("svc1", []string{"FOO=bar"}))
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	body := `[{"op":"test","path":"/FOO","value":"wrong"}]`
	req := httptest.NewRequest("PATCH", "/services/svc1/env", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceEnv(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status=%d, want 409", w.Code)
	}
}

func TestHandlePatchServiceEnv_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	body := `[{"op":"add","path":"/K","value":"v"}]`
	req := httptest.NewRequest("PATCH", "/services/missing/env", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json-patch+json")
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandlePatchServiceEnv(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandlePatchServiceEnv_MergePatch(t *testing.T) {
	c := cache.New(nil)
	c.SetService(serviceWithEnv("svc1", []string{"FOO=bar", "OLD=remove"}))

	wc := &mockWriteClient{
		mockServiceSpecWriter: mockServiceSpecWriter{
			updateServiceEnvFn: func(_ context.Context, id string, env map[string]string) (swarm.Service, error) {
				envSlice := make([]string, 0, len(env))
				for k, v := range env {
					envSlice = append(envSlice, k+"="+v)
				}
				return serviceWithEnv(id, envSlice), nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"NEW":"val","OLD":null}`
	req := httptest.NewRequest("PATCH", "/services/svc1/env", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceEnv(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "ServiceEnv" {
		t.Errorf("@type=%v, want ServiceEnv", resp["@type"])
	}
	if id, ok := resp["@id"].(string); !ok || !strings.HasSuffix(id, "/services/svc1/env") {
		t.Errorf("expected @id ending in /services/svc1/env, got %v", resp["@id"])
	}
	if ctx, ok := resp["@context"].(string); !ok || !strings.HasSuffix(ctx, "/api/context.jsonld") {
		t.Errorf("expected @context ending in /api/context.jsonld, got %v", resp["@context"])
	}
}

func TestHandleGetNodeLabels(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID: "node1",
		Spec: swarm.NodeSpec{
			Annotations: swarm.Annotations{Labels: map[string]string{"region": "us-east"}},
		},
	})
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/nodes/node1/labels", nil)
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleGetNodeLabels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "NodeLabels" {
		t.Errorf("@type=%v, want NodeLabels", resp["@type"])
	}
	labels, ok := resp["labels"].(map[string]any)
	if !ok {
		t.Fatal("expected labels key in response")
	}
	if labels["region"] != "us-east" {
		t.Errorf("region=%v, want us-east", labels["region"])
	}
}

func TestHandlePatchNodeLabels_Add(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID: "node1",
		Spec: swarm.NodeSpec{
			Annotations: swarm.Annotations{Labels: map[string]string{"existing": "value"}},
		},
	})

	wc := &mockWriteClient{
		mockNodeWriter: mockNodeWriter{
			updateNodeLabelsFn: func(_ context.Context, id string, labels map[string]string) (swarm.Node, error) {
				return swarm.Node{
					ID:   id,
					Spec: swarm.NodeSpec{Annotations: swarm.Annotations{Labels: labels}},
				}, nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `[{"op":"add","path":"/new","value":"label"}]`
	req := httptest.NewRequest("PATCH", "/nodes/node1/labels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json-patch+json")
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandlePatchNodeLabels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "NodeLabels" {
		t.Errorf("@type=%v, want NodeLabels", resp["@type"])
	}
	if id, ok := resp["@id"].(string); !ok || !strings.HasSuffix(id, "/nodes/node1/labels") {
		t.Errorf("expected @id ending in /nodes/node1/labels, got %v", resp["@id"])
	}
	if ctx, ok := resp["@context"].(string); !ok || !strings.HasSuffix(ctx, "/api/context.jsonld") {
		t.Errorf("expected @context ending in /api/context.jsonld, got %v", resp["@context"])
	}
}

func TestHandlePatchNodeLabels_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node1"})
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("PATCH", "/nodes/node1/labels", strings.NewReader(`[]`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandlePatchNodeLabels(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}

func TestHandlePatchNodeLabels_MergePatch(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID: "node1",
		Spec: swarm.NodeSpec{
			Annotations: swarm.Annotations{
				Labels: map[string]string{"existing": "value", "remove": "me"},
			},
		},
	})

	wc := &mockWriteClient{
		mockNodeWriter: mockNodeWriter{
			updateNodeLabelsFn: func(_ context.Context, id string, labels map[string]string) (swarm.Node, error) {
				return swarm.Node{
					ID:   id,
					Spec: swarm.NodeSpec{Annotations: swarm.Annotations{Labels: labels}},
				}, nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"new":"label","remove":null}`
	req := httptest.NewRequest("PATCH", "/nodes/node1/labels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandlePatchNodeLabels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetServiceResources(t *testing.T) {
	c := cache.New(nil)
	svc := replicatedService("svc1")
	svc.Spec.TaskTemplate.Resources = &swarm.ResourceRequirements{
		Limits: &swarm.Limit{NanoCPUs: 1000000000},
	}
	c.SetService(svc)
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/services/svc1/resources", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceResources(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "ServiceResources" {
		t.Errorf("@type=%v, want ServiceResources", resp["@type"])
	}
	resources, ok := resp["resources"].(map[string]any)
	if !ok {
		t.Fatal("expected resources key in response")
	}
	if resources["Limits"] == nil {
		t.Error("expected Limits in resources")
	}
}

func TestHandlePatchServiceResources_Merge(t *testing.T) {
	c := cache.New(nil)
	svc := replicatedService("svc1")
	svc.Spec.TaskTemplate.Resources = &swarm.ResourceRequirements{}
	c.SetService(svc)

	wc := &mockWriteClient{
		mockServiceSpecWriter: mockServiceSpecWriter{
			updateServiceResourcesFn: func(_ context.Context, id string, resources *swarm.ResourceRequirements) (swarm.Service, error) {
				s := replicatedService(id)
				s.Spec.TaskTemplate.Resources = resources
				return s, nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"Limits":{"NanoCPUs":500000000}}`
	req := httptest.NewRequest("PATCH", "/services/svc1/resources", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceResources(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "ServiceResources" {
		t.Errorf("@type=%v, want ServiceResources", resp["@type"])
	}
	if id, ok := resp["@id"].(string); !ok || !strings.HasSuffix(id, "/services/svc1/resources") {
		t.Errorf("expected @id ending in /services/svc1/resources, got %v", resp["@id"])
	}
	if ctx, ok := resp["@context"].(string); !ok || !strings.HasSuffix(ctx, "/api/context.jsonld") {
		t.Errorf("expected @context ending in /api/context.jsonld, got %v", resp["@context"])
	}
}

func TestHandlePatchServiceResources_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("PATCH", "/services/svc1/resources", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceResources(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}

func serviceWithHealthcheck(id string, hc *container.HealthConfig) swarm.Service {
	svc := replicatedService(id)
	svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Healthcheck: hc,
	}
	return svc
}

func TestHandleGetServiceHealthcheck(t *testing.T) {
	c := cache.New(nil)
	c.SetService(serviceWithHealthcheck("svc1", &container.HealthConfig{
		Test:     []string{"CMD", "curl", "-f", "http://localhost/"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}))
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/services/svc1/healthcheck", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceHealthcheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if resp["@type"] != "ServiceHealthcheck" {
		t.Errorf("@type=%v, want ServiceHealthcheck", resp["@type"])
	}
}

func TestHandleGetServiceHealthcheck_Nil(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/services/svc1/healthcheck", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceHealthcheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if resp["@type"] != "ServiceHealthcheck" {
		t.Errorf("@type=%v, want ServiceHealthcheck", resp["@type"])
	}
}

func TestHandlePutServiceHealthcheck(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{
		mockServiceSpecWriter: mockServiceSpecWriter{
			updateServiceHealthcheckFn: func(_ context.Context, id string, hc *container.HealthConfig) (swarm.Service, error) {
				return serviceWithHealthcheck(id, hc), nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"Test":["CMD","curl","-f","http://localhost/"],"Interval":10000000000,"Timeout":5000000000,"Retries":3}`
	req := httptest.NewRequest("PUT", "/services/svc1/healthcheck", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePutServiceHealthcheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if resp["@type"] != "ServiceHealthcheck" {
		t.Errorf("@type=%v, want ServiceHealthcheck", resp["@type"])
	}
}

func TestHandlePutServiceHealthcheck_Disable(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	var captured *container.HealthConfig
	wc := &mockWriteClient{
		mockServiceSpecWriter: mockServiceSpecWriter{
			updateServiceHealthcheckFn: func(_ context.Context, id string, hc *container.HealthConfig) (swarm.Service, error) {
				captured = hc
				return serviceWithHealthcheck(id, hc), nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"Test":["NONE"]}`
	req := httptest.NewRequest("PUT", "/services/svc1/healthcheck", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePutServiceHealthcheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}

	if captured == nil {
		t.Fatal("expected healthcheck to be captured")
	}

	if len(captured.Test) == 0 || captured.Test[0] != "NONE" {
		t.Errorf("Test[0]=%v, want NONE", captured.Test)
	}
}

func TestHandlePatchServiceHealthcheck_Merge(t *testing.T) {
	c := cache.New(nil)
	c.SetService(serviceWithHealthcheck("svc1", &container.HealthConfig{
		Test:     []string{"CMD", "curl", "-f", "http://localhost/"},
		Interval: 10 * time.Second,
		Timeout:  3 * time.Second,
		Retries:  3,
	}))

	var captured *container.HealthConfig
	wc := &mockWriteClient{
		mockServiceSpecWriter: mockServiceSpecWriter{
			updateServiceHealthcheckFn: func(_ context.Context, id string, hc *container.HealthConfig) (swarm.Service, error) {
				captured = hc
				return serviceWithHealthcheck(id, hc), nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"Timeout":5000000000}`
	req := httptest.NewRequest("PATCH", "/services/svc1/healthcheck", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceHealthcheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}

	if captured == nil {
		t.Fatal("expected healthcheck to be captured")
	}

	if captured.Timeout != 5*time.Second {
		t.Errorf("Timeout=%v, want 5s", captured.Timeout)
	}

	if len(captured.Test) == 0 || captured.Test[0] != "CMD" {
		t.Errorf("Test=%v, want [CMD curl -f http://localhost/]", captured.Test)
	}
}

func TestHandlePatchServiceHealthcheck_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	svc := replicatedService("svc1")
	c.SetService(svc)
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("PATCH", "/services/svc1/healthcheck", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceHealthcheck(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d, want 415; body: %s", w.Code, w.Body.String())
	}
}

func TestHandlePutServiceHealthcheck_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	body := `{"Test":["CMD-SHELL","curl http://localhost/"]}`
	req := httptest.NewRequest("PUT", "/services/nonexistent/healthcheck", strings.NewReader(body))
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()
	h.HandlePutServiceHealthcheck(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetServicePlacement(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				Placement: &swarm.Placement{
					Constraints: []string{"node.role==manager"},
				},
			},
		},
	})

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/services/svc1/placement", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServicePlacement(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	placement := resp["placement"].(map[string]any)
	constraints := placement["Constraints"].([]any)
	if len(constraints) != 1 || constraints[0] != "node.role==manager" {
		t.Errorf("unexpected placement: %v", resp)
	}
}

func TestHandleGetServicePlacement_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/services/missing/placement", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetServicePlacement(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandlePutServicePlacement(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	updated := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				Placement: &swarm.Placement{
					Constraints: []string{"node.role==worker"},
				},
			},
		},
	}
	mock := &mockWriteClient{
		mockServiceSpecWriter: mockServiceSpecWriter{
			updateServicePlacementFn: func(ctx context.Context, id string, placement *swarm.Placement) (swarm.Service, error) {
				return updated, nil
			},
		},
	}

	h := newTestHandlers(t, withCache(c), withWriteClient(mock))
	body := strings.NewReader(`{"Constraints":["node.role==worker"]}`)
	req := httptest.NewRequest("PUT", "/services/svc1/placement", body)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePutServicePlacement(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
}

func TestHandlePutServicePlacement_InvalidBody(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))
	body := strings.NewReader(`not json`)
	req := httptest.NewRequest("PUT", "/services/svc1/placement", body)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePutServicePlacement(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandlePutServicePlacement_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))
	body := strings.NewReader(`{"Constraints":[]}`)
	req := httptest.NewRequest("PUT", "/services/missing/placement", body)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandlePutServicePlacement(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleGetServicePorts(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			EndpointSpec: &swarm.EndpointSpec{
				Ports: []swarm.PortConfig{
					{
						Protocol:      "tcp",
						TargetPort:    80,
						PublishedPort: 8080,
						PublishMode:   swarm.PortConfigPublishModeIngress,
					},
				},
			},
		},
	})

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/services/svc1/ports", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServicePorts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
}

func TestHandleGetServicePorts_NilEndpointSpec(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/services/svc1/ports", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServicePorts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ports := resp["ports"].([]any)
	if len(ports) != 0 {
		t.Errorf("expected empty ports, got %v", ports)
	}
}

func TestHandlePatchServicePorts(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	updated := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			EndpointSpec: &swarm.EndpointSpec{
				Ports: []swarm.PortConfig{
					{Protocol: "tcp", TargetPort: 80, PublishedPort: 9090},
				},
			},
		},
	}
	mock := &mockWriteClient{
		mockServiceSpecWriter: mockServiceSpecWriter{
			updateServicePortsFn: func(ctx context.Context, id string, ports []swarm.PortConfig) (swarm.Service, error) {
				return updated, nil
			},
		},
	}

	h := newTestHandlers(t, withCache(c), withWriteClient(mock))
	body := strings.NewReader(`{"ports":[{"Protocol":"tcp","TargetPort":80,"PublishedPort":9090}]}`)
	req := httptest.NewRequest("PATCH", "/services/svc1/ports", body)
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServicePorts(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
}

func TestHandlePatchServicePorts_InvalidBody(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))
	body := strings.NewReader(`not json`)
	req := httptest.NewRequest("PATCH", "/services/svc1/ports", body)
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServicePorts(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandlePatchServicePorts_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))
	body := strings.NewReader(`{}`)
	req := httptest.NewRequest("PATCH", "/services/svc1/ports", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServicePorts(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}

func TestHandleGetServiceUpdatePolicy(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			UpdateConfig: &swarm.UpdateConfig{
				Parallelism: 2,
				Order:       "start-first",
			},
		},
	})

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/services/svc1/update-policy", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceUpdatePolicy(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
}

func TestHandlePatchServiceUpdatePolicy(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	mock := &mockWriteClient{
		mockServiceSpecWriter: mockServiceSpecWriter{
			updateServiceUpdatePolicyFn: func(ctx context.Context, id string, policy *swarm.UpdateConfig) (swarm.Service, error) {
				return swarm.Service{ID: "svc1", Spec: swarm.ServiceSpec{UpdateConfig: policy}}, nil
			},
		},
	}

	h := newTestHandlers(t, withCache(c), withWriteClient(mock))
	body := strings.NewReader(`{"Order":"start-first"}`)
	req := httptest.NewRequest("PATCH", "/services/svc1/update-policy", body)
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceUpdatePolicy(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
}

func TestHandlePatchServiceUpdatePolicy_InvalidBody(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))
	req := httptest.NewRequest(
		"PATCH",
		"/services/svc1/update-policy",
		strings.NewReader(`not json`),
	)
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceUpdatePolicy(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandlePatchServiceUpdatePolicy_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))
	req := httptest.NewRequest("PATCH", "/services/svc1/update-policy", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceUpdatePolicy(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}

func TestHandleGetServiceRollbackPolicy(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			RollbackConfig: &swarm.UpdateConfig{
				Parallelism: 1,
				Order:       "stop-first",
			},
		},
	})

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/services/svc1/rollback-policy", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceRollbackPolicy(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
}

func TestHandlePatchServiceRollbackPolicy_InvalidBody(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))
	req := httptest.NewRequest(
		"PATCH",
		"/services/svc1/rollback-policy",
		strings.NewReader(`not json`),
	)
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceRollbackPolicy(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandlePatchServiceRollbackPolicy(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	mock := &mockWriteClient{
		mockServiceSpecWriter: mockServiceSpecWriter{
			updateServiceRollbackPolicyFn: func(ctx context.Context, id string, policy *swarm.UpdateConfig) (swarm.Service, error) {
				return swarm.Service{
					ID:   "svc1",
					Spec: swarm.ServiceSpec{RollbackConfig: policy},
				}, nil
			},
		},
	}

	h := newTestHandlers(t, withCache(c), withWriteClient(mock))
	body := strings.NewReader(`{"FailureAction":"continue"}`)
	req := httptest.NewRequest("PATCH", "/services/svc1/rollback-policy", body)
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceRollbackPolicy(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
}

func TestHandleGetServiceLogDriver(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				LogDriver: &swarm.Driver{
					Name:    "json-file",
					Options: map[string]string{"max-size": "10m"},
				},
			},
		},
	})

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/services/svc1/log-driver", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceLogDriver(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
}

func TestHandleGetServiceLogDriver_Nil(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/services/svc1/log-driver", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceLogDriver(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["logDriver"] != nil {
		t.Errorf("expected null logDriver, got %v", resp["logDriver"])
	}
}

func TestHandlePatchServiceLogDriver(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				LogDriver: &swarm.Driver{
					Name:    "json-file",
					Options: map[string]string{"max-size": "10m"},
				},
			},
		},
	})

	mock := &mockWriteClient{
		mockServiceSpecWriter: mockServiceSpecWriter{
			updateServiceLogDriverFn: func(ctx context.Context, id string, driver *swarm.Driver) (swarm.Service, error) {
				return swarm.Service{
					ID:   "svc1",
					Spec: swarm.ServiceSpec{TaskTemplate: swarm.TaskSpec{LogDriver: driver}},
				}, nil
			},
		},
	}

	h := newTestHandlers(t, withCache(c), withWriteClient(mock))
	body := strings.NewReader(`{"Options":{"max-size":"20m"}}`)
	req := httptest.NewRequest("PATCH", "/services/svc1/log-driver", body)
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceLogDriver(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
}

func TestHandlePatchServiceLogDriver_InvalidBody(t *testing.T) {
	c := cache.New(nil)
	c.SetService(
		swarm.Service{
			ID: "svc1",
			Spec: swarm.ServiceSpec{
				TaskTemplate: swarm.TaskSpec{LogDriver: &swarm.Driver{Name: "json-file"}},
			},
		},
	)

	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))
	req := httptest.NewRequest("PATCH", "/services/svc1/log-driver", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceLogDriver(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleUpdateNodeRole_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node1", Spec: swarm.NodeSpec{Role: swarm.NodeRoleWorker}})

	wc := &mockWriteClient{
		mockNodeWriter: mockNodeWriter{
			updateNodeRoleFn: func(_ context.Context, id string, role swarm.NodeRole) (swarm.Node, error) {
				return swarm.Node{ID: id, Spec: swarm.NodeSpec{Role: role}}, nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"role":"manager"}`
	req := httptest.NewRequest("PUT", "/nodes/node1/role", strings.NewReader(body))
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleUpdateNodeRole(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "Node" {
		t.Errorf("@type=%v, want Node", resp["@type"])
	}
}

func TestHandleUpdateNodeRole_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"role":"manager"}`
	req := httptest.NewRequest("PUT", "/nodes/missing/role", strings.NewReader(body))
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleUpdateNodeRole(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleUpdateNodeRole_InvalidRole(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node1"})
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"role":"invalid"}`
	req := httptest.NewRequest("PUT", "/nodes/node1/role", strings.NewReader(body))
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleUpdateNodeRole(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleUpdateNodeRole_Conflict(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node1"})

	wc := &mockWriteClient{
		mockNodeWriter: mockNodeWriter{
			updateNodeRoleFn: func(_ context.Context, _ string, _ swarm.NodeRole) (swarm.Node, error) {
				return swarm.Node{}, errdefs.Conflict(fmt.Errorf("conflict"))
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"role":"manager"}`
	req := httptest.NewRequest("PUT", "/nodes/node1/role", strings.NewReader(body))
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleUpdateNodeRole(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status=%d, want 409", w.Code)
	}
}

func TestHandleRemoveNode_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node1"})

	wc := &mockWriteClient{
		mockNodeWriter: mockNodeWriter{
			removeNodeFn: func(_ context.Context, _ string, _ bool) error {
				return nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/nodes/node1", nil)
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleRemoveNode(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status=%d, want 204; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleRemoveNode_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/nodes/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleRemoveNode(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleRemoveNode_DockerError(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node1"})

	wc := &mockWriteClient{
		mockNodeWriter: mockNodeWriter{
			removeNodeFn: func(_ context.Context, _ string, _ bool) error {
				return fmt.Errorf("node is not down")
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/nodes/node1", nil)
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleRemoveNode(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", w.Code)
	}
}

func TestHandleRemoveNode_Force(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node1"})

	var gotForce bool
	wc := &mockWriteClient{
		mockNodeWriter: mockNodeWriter{
			removeNodeFn: func(_ context.Context, _ string, force bool) error {
				gotForce = force
				return nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/nodes/node1?force=true", nil)
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleRemoveNode(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status=%d, want 204; body: %s", w.Code, w.Body.String())
	}
	if !gotForce {
		t.Error("expected force=true to be passed to client")
	}
}

func TestHandleRemoveVolume_Force(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "my-vol"})

	var gotForce bool
	wc := &mockWriteClient{
		mockResourceRemover: mockResourceRemover{
			removeVolumeFn: func(_ context.Context, _ string, force bool) error {
				gotForce = force
				return nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/volumes/my-vol?force=true", nil)
	req.SetPathValue("name", "my-vol")
	w := httptest.NewRecorder()
	h.HandleRemoveVolume(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body: %s", w.Code, w.Body.String())
	}
	if !gotForce {
		t.Error("expected force=true to be passed to client")
	}
}

func TestHandleGetNodeRole_Manager(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:            "node1",
		Spec:          swarm.NodeSpec{Role: swarm.NodeRoleManager},
		ManagerStatus: &swarm.ManagerStatus{Leader: true},
	})
	c.SetNode(swarm.Node{
		ID:   "node2",
		Spec: swarm.NodeSpec{Role: swarm.NodeRoleManager},
	})
	c.SetNode(swarm.Node{
		ID:   "node3",
		Spec: swarm.NodeSpec{Role: swarm.NodeRoleWorker},
	})

	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/nodes/node1/role", nil)
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleGetNodeRole(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["role"] != "manager" {
		t.Errorf("role=%v, want manager", resp["role"])
	}
	if resp["isLeader"] != true {
		t.Errorf("isLeader=%v, want true", resp["isLeader"])
	}
	if resp["managerCount"] != float64(2) {
		t.Errorf("managerCount=%v, want 2", resp["managerCount"])
	}
}

func TestHandleGetNodeRole_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/nodes/missing/role", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetNodeRole(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func seedStack(c *cache.Cache, name string) {
	label := map[string]string{"com.docker.stack.namespace": name}
	c.SetService(swarm.Service{
		ID: name + "_svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: name + "_svc1", Labels: label},
		},
	})
	c.SetService(swarm.Service{
		ID: name + "_svc2",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: name + "_svc2", Labels: label},
		},
	})
	c.SetNetwork(network.Summary{ID: name + "_net1", Name: name + "_net1", Labels: label})
	c.SetConfig(swarm.Config{
		ID:   name + "_cfg1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: name + "_cfg1", Labels: label}},
	})
	c.SetSecret(swarm.Secret{
		ID:   name + "_sec1",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: name + "_sec1", Labels: label}},
	})
}

func TestHandleRemoveStack_OK(t *testing.T) {
	c := cache.New(nil)
	seedStack(c, "myapp")

	wc := &mockWriteClient{
		mockServiceLifecycleWriter: mockServiceLifecycleWriter{
			removeServiceFn: func(_ context.Context, _ string) error { return nil },
		},
		mockResourceRemover: mockResourceRemover{
			removeNetworkFn: func(_ context.Context, _ string) error { return nil },
		},
		mockConfigWriter: mockConfigWriter{
			removeConfigFn: func(_ context.Context, _ string) error { return nil },
		},
		mockSecretWriter: mockSecretWriter{
			removeSecretFn: func(_ context.Context, _ string) error { return nil },
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/stacks/myapp", nil)
	req.SetPathValue("name", "myapp")
	w := httptest.NewRecorder()
	h.HandleRemoveStack(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	removed := resp["removed"].(map[string]any)
	if removed["services"] != float64(2) {
		t.Errorf("services=%v, want 2", removed["services"])
	}
	if removed["networks"] != float64(1) {
		t.Errorf("networks=%v, want 1", removed["networks"])
	}
	if resp["errors"] != nil {
		t.Errorf("errors=%v, want nil", resp["errors"])
	}
}

func TestHandleRemoveStack_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("DELETE", "/stacks/missing", nil)
	req.SetPathValue("name", "missing")
	w := httptest.NewRecorder()
	h.HandleRemoveStack(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleRemoveStack_PartialFailure(t *testing.T) {
	c := cache.New(nil)
	seedStack(c, "myapp")

	wc := &mockWriteClient{
		mockServiceLifecycleWriter: mockServiceLifecycleWriter{
			removeServiceFn: func(_ context.Context, _ string) error { return nil },
		},
		mockResourceRemover: mockResourceRemover{
			removeNetworkFn: func(_ context.Context, _ string) error {
				return fmt.Errorf("network is in use")
			},
		},
		mockConfigWriter: mockConfigWriter{
			removeConfigFn: func(_ context.Context, _ string) error { return nil },
		},
		mockSecretWriter: mockSecretWriter{
			removeSecretFn: func(_ context.Context, _ string) error { return nil },
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/stacks/myapp", nil)
	req.SetPathValue("name", "myapp")
	w := httptest.NewRecorder()
	h.HandleRemoveStack(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["errors"] == nil {
		t.Fatal("expected errors array")
	}
	errs := resp["errors"].([]any)
	if len(errs) != 1 {
		t.Fatalf("errors length=%d, want 1", len(errs))
	}
}

func TestHandleRemoveStack_AlreadyGone(t *testing.T) {
	c := cache.New(nil)
	seedStack(c, "myapp")

	wc := &mockWriteClient{
		mockServiceLifecycleWriter: mockServiceLifecycleWriter{
			removeServiceFn: func(_ context.Context, _ string) error {
				return errdefs.NotFound(fmt.Errorf("not found"))
			},
		},
		mockResourceRemover: mockResourceRemover{
			removeNetworkFn: func(_ context.Context, _ string) error {
				return errdefs.NotFound(fmt.Errorf("not found"))
			},
		},
		mockConfigWriter: mockConfigWriter{
			removeConfigFn: func(_ context.Context, _ string) error {
				return errdefs.NotFound(fmt.Errorf("not found"))
			},
		},
		mockSecretWriter: mockSecretWriter{
			removeSecretFn: func(_ context.Context, _ string) error {
				return errdefs.NotFound(fmt.Errorf("not found"))
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/stacks/myapp", nil)
	req.SetPathValue("name", "myapp")
	w := httptest.NewRecorder()
	h.HandleRemoveStack(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	removed := resp["removed"].(map[string]any)
	if removed["services"] != float64(0) {
		t.Errorf("services=%v, want 0 (all were already gone)", removed["services"])
	}
	if resp["errors"] != nil {
		t.Errorf("errors=%v, want nil (404s are skipped)", resp["errors"])
	}
}

func TestHandleGetServiceContainerConfig_OK(t *testing.T) {
	init := true
	gracePeriod := time.Duration(10_000_000_000)
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Command:         []string{"/bin/sh"},
					Args:            []string{"-c", "echo hello"},
					Dir:             "/app",
					User:            "node",
					Hostname:        "web-1",
					Init:            &init,
					TTY:             false,
					ReadOnly:        true,
					StopSignal:      "SIGTERM",
					StopGracePeriod: &gracePeriod,
					CapabilityAdd:   []string{"NET_ADMIN"},
					CapabilityDrop:  []string{"ALL"},
				},
			},
		},
	})

	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/services/svc1/container-config", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceContainerConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["hostname"] != "web-1" {
		t.Errorf("hostname=%v, want web-1", resp["hostname"])
	}
	if resp["readOnly"] != true {
		t.Errorf("readOnly=%v, want true", resp["readOnly"])
	}
	if resp["stopGracePeriod"] != float64(10_000_000_000) {
		t.Errorf("stopGracePeriod=%v, want 10000000000", resp["stopGracePeriod"])
	}
}

func TestHandleGetServiceContainerConfig_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/services/missing/container-config", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetServiceContainerConfig(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandlePatchServiceContainerConfig_PartialPatch(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Hostname: "old-host",
					TTY:      false,
				},
			},
		},
	})

	wc := &mockWriteClient{
		mockServiceAttachmentWriter: mockServiceAttachmentWriter{
			updateServiceContainerConfigFn: func(_ context.Context, id string, apply func(*swarm.ContainerSpec)) (swarm.Service, error) {
				cs := &swarm.ContainerSpec{}
				apply(cs)
				return swarm.Service{
					ID: id,
					Spec: swarm.ServiceSpec{
						TaskTemplate: swarm.TaskSpec{ContainerSpec: cs},
					},
				}, nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"hostname":"new-host","tty":true}`
	req := httptest.NewRequest("PATCH", "/services/svc1/container-config", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	req.Header.Set("Content-Type", "application/merge-patch+json")
	w := httptest.NewRecorder()
	h.HandlePatchServiceContainerConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	cc, ok := resp["containerConfig"].(map[string]any)
	if !ok {
		t.Fatalf("containerConfig missing or wrong type; body: %s", w.Body.String())
	}
	if cc["hostname"] != "new-host" {
		t.Errorf("hostname=%v, want new-host", cc["hostname"])
	}
	if cc["tty"] != true {
		t.Errorf("tty=%v, want true", cc["tty"])
	}
	if resp["@type"] != "ServiceContainerConfig" {
		t.Errorf("@type=%v, want ServiceContainerConfig", resp["@type"])
	}
	if id, ok := resp["@id"].(string); !ok ||
		!strings.HasSuffix(id, "/services/svc1/container-config") {
		t.Errorf("expected @id ending in /services/svc1/container-config, got %v", resp["@id"])
	}
	if ctx, ok := resp["@context"].(string); !ok || !strings.HasSuffix(ctx, "/api/context.jsonld") {
		t.Errorf("expected @context ending in /api/context.jsonld, got %v", resp["@context"])
	}
}

func TestHandlePatchServiceContainerConfig_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetService(
		swarm.Service{
			ID: "svc1",
			Spec: swarm.ServiceSpec{
				TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{}},
			},
		},
	)
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	body := `{"hostname":"x"}`
	req := httptest.NewRequest("PATCH", "/services/svc1/container-config", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandlePatchServiceContainerConfig(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}

func TestHandlePatchServiceContainerConfig_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest(
		"PATCH",
		"/services/missing/container-config",
		strings.NewReader(`{}`),
	)
	req.SetPathValue("id", "missing")
	req.Header.Set("Content-Type", "application/merge-patch+json")
	w := httptest.NewRecorder()
	h.HandlePatchServiceContainerConfig(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleGetServiceConfigs_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Configs: []*swarm.ConfigReference{
						{
							ConfigID:   "cfg1",
							ConfigName: "app-config",
							File:       &swarm.ConfigReferenceFileTarget{Name: "/etc/app.yaml"},
						},
					},
				},
			},
		},
	})
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/services/svc1/configs", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceConfigs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	configs := resp["configs"].([]any)
	if len(configs) != 1 {
		t.Fatalf("len(configs)=%d, want 1", len(configs))
	}
	cfg := configs[0].(map[string]any)
	if cfg["configID"] != "cfg1" {
		t.Errorf("configID=%v, want cfg1", cfg["configID"])
	}
	if cfg["fileName"] != "/etc/app.yaml" {
		t.Errorf("fileName=%v, want /etc/app.yaml", cfg["fileName"])
	}
}

func TestHandleGetServiceConfigs_Empty(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/services/svc1/configs", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceConfigs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	configs := resp["configs"].([]any)
	if len(configs) != 0 {
		t.Errorf("len(configs)=%d, want 0", len(configs))
	}
}

func TestHandleGetServiceConfigs_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/services/missing/configs", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetServiceConfigs(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandlePatchServiceConfigs_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	updated := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Configs: []*swarm.ConfigReference{
						{
							ConfigID:   "cfg1",
							ConfigName: "app-config",
							File:       &swarm.ConfigReferenceFileTarget{Name: "/app.yaml"},
						},
					},
				},
			},
		},
	}
	mock := &mockWriteClient{
		mockServiceAttachmentWriter: mockServiceAttachmentWriter{
			updateServiceConfigsFn: func(_ context.Context, _ string, _ []*swarm.ConfigReference) (swarm.Service, error) {
				return updated, nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(mock))

	body := `{"configs":[{"configID":"cfg1","configName":"app-config","fileName":"/app.yaml"}]}`
	req := httptest.NewRequest("PATCH", "/services/svc1/configs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceConfigs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandlePatchServiceConfigs_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	body := `{"configs":[]}`
	req := httptest.NewRequest("PATCH", "/services/svc1/configs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceConfigs(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}

func TestHandleGetServiceSecrets_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Secrets: []*swarm.SecretReference{
						{
							SecretID:   "sec1",
							SecretName: "db-password",
							File: &swarm.SecretReferenceFileTarget{
								Name: "/run/secrets/db-password",
							},
						},
					},
				},
			},
		},
	})
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/services/svc1/secrets", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceSecrets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	secrets := resp["secrets"].([]any)
	if len(secrets) != 1 {
		t.Fatalf("len(secrets)=%d, want 1", len(secrets))
	}
	sec := secrets[0].(map[string]any)
	if sec["secretID"] != "sec1" {
		t.Errorf("secretID=%v, want sec1", sec["secretID"])
	}
	if sec["fileName"] != "/run/secrets/db-password" {
		t.Errorf("fileName=%v, want /run/secrets/db-password", sec["fileName"])
	}
}

func TestHandleGetServiceSecrets_Empty(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/services/svc1/secrets", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceSecrets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	secrets := resp["secrets"].([]any)
	if len(secrets) != 0 {
		t.Errorf("len(secrets)=%d, want 0", len(secrets))
	}
}

func TestHandleGetServiceSecrets_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/services/missing/secrets", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetServiceSecrets(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandlePatchServiceSecrets_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	updated := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Secrets: []*swarm.SecretReference{
						{
							SecretID:   "sec1",
							SecretName: "db-password",
							File: &swarm.SecretReferenceFileTarget{
								Name: "/run/secrets/db-password",
							},
						},
					},
				},
			},
		},
	}
	mock := &mockWriteClient{
		mockServiceAttachmentWriter: mockServiceAttachmentWriter{
			updateServiceSecretsFn: func(_ context.Context, _ string, _ []*swarm.SecretReference) (swarm.Service, error) {
				return updated, nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(mock))

	body := `{"secrets":[{"secretID":"sec1","secretName":"db-password","fileName":"/run/secrets/db-password"}]}`
	req := httptest.NewRequest("PATCH", "/services/svc1/secrets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceSecrets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandlePatchServiceSecrets_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	body := `{"secrets":[]}`
	req := httptest.NewRequest("PATCH", "/services/svc1/secrets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceSecrets(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}

func TestHandleGetServiceNetworks_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				Networks: []swarm.NetworkAttachmentConfig{
					{Target: "net1", Aliases: []string{"web"}},
				},
			},
		},
	})
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/services/svc1/networks", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceNetworks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	networks := resp["networks"].([]any)
	if len(networks) != 1 {
		t.Fatalf("len(networks)=%d, want 1", len(networks))
	}
	net := networks[0].(map[string]any)
	if net["target"] != "net1" {
		t.Errorf("target=%v, want net1", net["target"])
	}
	aliases := net["aliases"].([]any)
	if len(aliases) != 1 || aliases[0] != "web" {
		t.Errorf("aliases=%v, want [web]", aliases)
	}
}

func TestHandleGetServiceNetworks_Empty(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/services/svc1/networks", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceNetworks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	networks := resp["networks"].([]any)
	if len(networks) != 0 {
		t.Errorf("len(networks)=%d, want 0", len(networks))
	}
}

func TestHandleGetServiceNetworks_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/services/missing/networks", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetServiceNetworks(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandlePatchServiceNetworks_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	updated := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				Networks: []swarm.NetworkAttachmentConfig{
					{Target: "net1", Aliases: []string{"web"}},
				},
			},
		},
	}
	mock := &mockWriteClient{
		mockServiceAttachmentWriter: mockServiceAttachmentWriter{
			updateServiceNetworksFn: func(_ context.Context, _ string, _ []swarm.NetworkAttachmentConfig) (swarm.Service, error) {
				return updated, nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(mock))

	body := `{"networks":[{"target":"net1","aliases":["web"]}]}`
	req := httptest.NewRequest("PATCH", "/services/svc1/networks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceNetworks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandlePatchServiceNetworks_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	body := `{"networks":[]}`
	req := httptest.NewRequest("PATCH", "/services/svc1/networks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceNetworks(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}

func TestHandleGetServiceMounts_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Mounts: []mount.Mount{
						{Type: mount.TypeVolume, Source: "data", Target: "/data"},
					},
				},
			},
		},
	})
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/services/svc1/mounts", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceMounts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	mounts, ok := resp["mounts"].([]any)
	if !ok || len(mounts) != 1 {
		t.Fatalf("mounts=%v, want 1 mount", resp["mounts"])
	}
}

func TestHandleGetServiceMounts_Empty(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{},
			},
		},
	})
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/services/svc1/mounts", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceMounts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	mounts, ok := resp["mounts"].([]any)
	if !ok || len(mounts) != 0 {
		t.Fatalf("mounts=%v, want empty array", resp["mounts"])
	}
}

func TestHandleGetServiceMounts_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	req := httptest.NewRequest("GET", "/services/missing/mounts", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetServiceMounts(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandlePatchServiceMounts_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	wc := &mockWriteClient{
		mockServiceAttachmentWriter: mockServiceAttachmentWriter{
			updateServiceMountsFn: func(_ context.Context, _ string, mounts []mount.Mount) (swarm.Service, error) {
				return swarm.Service{
					ID: "svc1",
					Spec: swarm.ServiceSpec{
						TaskTemplate: swarm.TaskSpec{
							ContainerSpec: &swarm.ContainerSpec{Mounts: mounts},
						},
					},
				}, nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"mounts":[{"Type":"volume","Source":"data","Target":"/data"}]}`
	req := httptest.NewRequest("PATCH", "/services/svc1/mounts", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	req.Header.Set("Content-Type", "application/merge-patch+json")
	w := httptest.NewRecorder()
	h.HandlePatchServiceMounts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandlePatchServiceMounts_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	body := `{"mounts":[]}`
	req := httptest.NewRequest("PATCH", "/services/missing/mounts", strings.NewReader(body))
	req.SetPathValue("id", "missing")
	req.Header.Set("Content-Type", "application/merge-patch+json")
	w := httptest.NewRecorder()
	h.HandlePatchServiceMounts(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandlePatchServiceMounts_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})
	h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

	body := `{"mounts":[]}`
	req := httptest.NewRequest("PATCH", "/services/svc1/mounts", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandlePatchServiceMounts(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}

func TestHandleRemoveConfig_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{
		ID:   "cfg1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "my-config"}},
	})

	wc := &mockWriteClient{
		mockConfigWriter: mockConfigWriter{
			removeConfigFn: func(_ context.Context, id string) error {
				return nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/configs/cfg1", nil)
	req.SetPathValue("id", "cfg1")
	w := httptest.NewRecorder()
	h.HandleRemoveConfig(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleRemoveConfig_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/configs/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleRemoveConfig(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleRemoveConfig_DockerError(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{
		ID:   "cfg1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "my-config"}},
	})

	wc := &mockWriteClient{
		mockConfigWriter: mockConfigWriter{
			removeConfigFn: func(_ context.Context, id string) error {
				return fmt.Errorf("engine error")
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/configs/cfg1", nil)
	req.SetPathValue("id", "cfg1")
	w := httptest.NewRecorder()
	h.HandleRemoveConfig(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", w.Code)
	}
}

func TestHandleRemoveSecret_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{
		ID:   "sec1",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "my-secret"}},
	})

	wc := &mockWriteClient{
		mockSecretWriter: mockSecretWriter{
			removeSecretFn: func(_ context.Context, id string) error {
				return nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/secrets/sec1", nil)
	req.SetPathValue("id", "sec1")
	w := httptest.NewRecorder()
	h.HandleRemoveSecret(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleRemoveSecret_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/secrets/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleRemoveSecret(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleRemoveSecret_DockerError(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{
		ID:   "sec1",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "my-secret"}},
	})

	wc := &mockWriteClient{
		mockSecretWriter: mockSecretWriter{
			removeSecretFn: func(_ context.Context, id string) error {
				return fmt.Errorf("engine error")
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/secrets/sec1", nil)
	req.SetPathValue("id", "sec1")
	w := httptest.NewRecorder()
	h.HandleRemoveSecret(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", w.Code)
	}
}

func TestHandleRemoveNetwork_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "my-network"})

	wc := &mockWriteClient{
		mockResourceRemover: mockResourceRemover{
			removeNetworkFn: func(_ context.Context, id string) error {
				return nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/networks/net1", nil)
	req.SetPathValue("id", "net1")
	w := httptest.NewRecorder()
	h.HandleRemoveNetwork(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleRemoveNetwork_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/networks/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleRemoveNetwork(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleRemoveNetwork_DockerError(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "my-network"})

	wc := &mockWriteClient{
		mockResourceRemover: mockResourceRemover{
			removeNetworkFn: func(_ context.Context, id string) error {
				return fmt.Errorf("engine error")
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/networks/net1", nil)
	req.SetPathValue("id", "net1")
	w := httptest.NewRecorder()
	h.HandleRemoveNetwork(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", w.Code)
	}
}

func TestHandleRemoveVolume_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "my-vol"})

	wc := &mockWriteClient{
		mockResourceRemover: mockResourceRemover{
			removeVolumeFn: func(_ context.Context, name string, _ bool) error {
				return nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/volumes/my-vol", nil)
	req.SetPathValue("name", "my-vol")
	w := httptest.NewRecorder()
	h.HandleRemoveVolume(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleRemoveVolume_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/volumes/missing", nil)
	req.SetPathValue("name", "missing")
	w := httptest.NewRecorder()
	h.HandleRemoveVolume(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleRemoveVolume_DockerError(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "my-vol"})

	wc := &mockWriteClient{
		mockResourceRemover: mockResourceRemover{
			removeVolumeFn: func(_ context.Context, name string, _ bool) error {
				return fmt.Errorf("engine error")
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	req := httptest.NewRequest("DELETE", "/volumes/my-vol", nil)
	req.SetPathValue("name", "my-vol")
	w := httptest.NewRecorder()
	h.HandleRemoveVolume(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", w.Code)
	}
}

func TestHandleCreateConfig_OK(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{
		mockConfigWriter: mockConfigWriter{
			createConfigFn: func(_ context.Context, spec swarm.ConfigSpec) (string, error) {
				return "new-cfg-id", nil
			},
		},
	}
	h := newTestHandlers(
		t,
		withCache(c),
		withWriteClient(wc),
		withOpsLevel(config.OpsConfiguration),
	)

	// Pre-populate cache so the post-create inspect works.
	c.SetConfig(swarm.Config{
		ID:   "new-cfg-id",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "my-config"}},
	})

	body := `{"name":"my-config","data":"aGVsbG8="}`
	req := httptest.NewRequest("POST", "/configs", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateConfig(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body: %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/configs/new-cfg-id" {
		t.Errorf("Location=%q, want /configs/new-cfg-id", loc)
	}
}

func TestHandleCreateConfig_MissingName(t *testing.T) {
	h := newTestHandlers(
		t,
		withWriteClient(&mockWriteClient{}),
		withOpsLevel(config.OpsConfiguration),
	)

	body := `{"data":"aGVsbG8="}`
	req := httptest.NewRequest("POST", "/configs", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleCreateConfig_InvalidBase64(t *testing.T) {
	h := newTestHandlers(
		t,
		withWriteClient(&mockWriteClient{}),
		withOpsLevel(config.OpsConfiguration),
	)

	body := `{"name":"my-config","data":"not-valid-base64!!!"}`
	req := httptest.NewRequest("POST", "/configs", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleCreateConfig_NameConflict(t *testing.T) {
	wc := &mockWriteClient{
		mockConfigWriter: mockConfigWriter{
			createConfigFn: func(_ context.Context, spec swarm.ConfigSpec) (string, error) {
				return "", errdefs.Conflict(fmt.Errorf("config already exists"))
			},
		},
	}
	h := newTestHandlers(t, withWriteClient(wc), withOpsLevel(config.OpsConfiguration))

	body := `{"name":"existing","data":"aGVsbG8="}`
	req := httptest.NewRequest("POST", "/configs", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateConfig(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status=%d, want 409; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateConfig_InvalidJSON(t *testing.T) {
	h := newTestHandlers(
		t,
		withWriteClient(&mockWriteClient{}),
		withOpsLevel(config.OpsConfiguration),
	)

	req := httptest.NewRequest("POST", "/configs", strings.NewReader("{invalid"))
	w := httptest.NewRecorder()
	h.HandleCreateConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleCreateSecret_OK(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{
		mockSecretWriter: mockSecretWriter{
			createSecretFn: func(_ context.Context, spec swarm.SecretSpec) (string, error) {
				return "new-sec-id", nil
			},
		},
	}
	h := newTestHandlers(
		t,
		withCache(c),
		withWriteClient(wc),
		withOpsLevel(config.OpsConfiguration),
	)

	c.SetSecret(swarm.Secret{
		ID:   "new-sec-id",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "my-secret"}},
	})

	body := `{"name":"my-secret","data":"c2VjcmV0"}`
	req := httptest.NewRequest("POST", "/secrets", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateSecret(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body: %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/secrets/new-sec-id" {
		t.Errorf("Location=%q, want /secrets/new-sec-id", loc)
	}
}

func TestHandleCreateSecret_MissingName(t *testing.T) {
	h := newTestHandlers(
		t,
		withWriteClient(&mockWriteClient{}),
		withOpsLevel(config.OpsConfiguration),
	)

	body := `{"data":"c2VjcmV0"}`
	req := httptest.NewRequest("POST", "/secrets", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateSecret(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleCreateSecret_NameConflict(t *testing.T) {
	wc := &mockWriteClient{
		mockSecretWriter: mockSecretWriter{
			createSecretFn: func(_ context.Context, spec swarm.SecretSpec) (string, error) {
				return "", errdefs.Conflict(fmt.Errorf("secret already exists"))
			},
		},
	}
	h := newTestHandlers(t, withWriteClient(wc), withOpsLevel(config.OpsConfiguration))

	body := `{"name":"existing","data":"c2VjcmV0"}`
	req := httptest.NewRequest("POST", "/secrets", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateSecret(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status=%d, want 409; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateSecret_InvalidBase64(t *testing.T) {
	h := newTestHandlers(
		t,
		withWriteClient(&mockWriteClient{}),
		withOpsLevel(config.OpsConfiguration),
	)

	body := `{"name":"my-secret","data":"not-valid-base64!!!"}`
	req := httptest.NewRequest("POST", "/secrets", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateSecret(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleCreateSecret_InvalidJSON(t *testing.T) {
	h := newTestHandlers(
		t,
		withWriteClient(&mockWriteClient{}),
		withOpsLevel(config.OpsConfiguration),
	)

	req := httptest.NewRequest("POST", "/secrets", strings.NewReader("{invalid"))
	w := httptest.NewRecorder()
	h.HandleCreateSecret(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleCreateSecret_ClearsData(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{
		mockSecretWriter: mockSecretWriter{
			createSecretFn: func(_ context.Context, spec swarm.SecretSpec) (string, error) {
				return "new-sec-id", nil
			},
		},
	}
	h := newTestHandlers(
		t,
		withCache(c),
		withWriteClient(wc),
		withOpsLevel(config.OpsConfiguration),
	)

	c.SetSecret(swarm.Secret{
		ID: "new-sec-id",
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{Name: "my-secret"},
			Data:        []byte("sensitive"),
		},
	})

	body := `{"name":"my-secret","data":"c2VjcmV0"}`
	req := httptest.NewRequest("POST", "/secrets", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateSecret(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body: %s", w.Code, w.Body.String())
	}

	// Verify the response does not contain secret data.
	respBody := w.Body.String()
	if strings.Contains(respBody, "sensitive") {
		t.Error("response contains secret data; expected it to be cleared")
	}
}

func TestHandleCreateConfig_WhitespaceOnlyName(t *testing.T) {
	h := newTestHandlers(
		t,
		withWriteClient(&mockWriteClient{}),
		withOpsLevel(config.OpsConfiguration),
	)

	body := `{"name":"   ","data":"aGVsbG8="}`
	req := httptest.NewRequest("POST", "/configs", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleCreateConfig_CacheMiss(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{
		mockConfigWriter: mockConfigWriter{
			createConfigFn: func(_ context.Context, spec swarm.ConfigSpec) (string, error) {
				return "new-cfg-id", nil
			},
		},
	}
	h := newTestHandlers(
		t,
		withCache(c),
		withWriteClient(wc),
		withOpsLevel(config.OpsConfiguration),
	)

	// Do NOT populate the cache — simulates the watcher not having caught up yet.
	body := `{"name":"my-config","data":"aGVsbG8="}`
	req := httptest.NewRequest("POST", "/configs", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateConfig(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body: %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/configs/new-cfg-id" {
		t.Errorf("Location=%q, want /configs/new-cfg-id", loc)
	}

	// Verify the response contains the minimal config with ID and name.
	respBody := w.Body.String()
	if !strings.Contains(respBody, "new-cfg-id") {
		t.Errorf("response missing config ID")
	}
}

func TestHandleCreateSecret_CacheMiss(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{
		mockSecretWriter: mockSecretWriter{
			createSecretFn: func(_ context.Context, spec swarm.SecretSpec) (string, error) {
				return "new-sec-id", nil
			},
		},
	}
	h := newTestHandlers(
		t,
		withCache(c),
		withWriteClient(wc),
		withOpsLevel(config.OpsConfiguration),
	)

	body := `{"name":"my-secret","data":"c2VjcmV0"}`
	req := httptest.NewRequest("POST", "/secrets", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateSecret(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body: %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/secrets/new-sec-id" {
		t.Errorf("Location=%q, want /secrets/new-sec-id", loc)
	}
}

func TestHandleCreateConfig_DockerError(t *testing.T) {
	wc := &mockWriteClient{
		mockConfigWriter: mockConfigWriter{
			createConfigFn: func(_ context.Context, spec swarm.ConfigSpec) (string, error) {
				return "", fmt.Errorf("engine error")
			},
		},
	}
	h := newTestHandlers(t, withWriteClient(wc), withOpsLevel(config.OpsConfiguration))

	body := `{"name":"my-config","data":"aGVsbG8="}`
	req := httptest.NewRequest("POST", "/configs", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateConfig(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", w.Code)
	}
}

func TestHandleCreateSecret_DockerError(t *testing.T) {
	wc := &mockWriteClient{
		mockSecretWriter: mockSecretWriter{
			createSecretFn: func(_ context.Context, spec swarm.SecretSpec) (string, error) {
				return "", fmt.Errorf("engine error")
			},
		},
	}
	h := newTestHandlers(t, withWriteClient(wc), withOpsLevel(config.OpsConfiguration))

	body := `{"name":"my-secret","data":"c2VjcmV0"}`
	req := httptest.NewRequest("POST", "/secrets", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateSecret(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", w.Code)
	}
}

func TestHandleGetConfigLabels(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{
		ID: "cfg1",
		Spec: swarm.ConfigSpec{
			Annotations: swarm.Annotations{
				Name:   "my-config",
				Labels: map[string]string{"env": "prod"},
			},
		},
	})
	h := newTestHandlers(
		t,
		withCache(c),
		withWriteClient(&mockWriteClient{}),
		withOpsLevel(config.OpsConfiguration),
	)

	req := httptest.NewRequest("GET", "/configs/cfg1/labels", nil)
	req.SetPathValue("id", "cfg1")
	w := httptest.NewRecorder()
	h.HandleGetConfigLabels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "ConfigLabels" {
		t.Errorf("@type=%v, want ConfigLabels", resp["@type"])
	}
	labels, ok := resp["labels"].(map[string]any)
	if !ok {
		t.Fatal("expected labels key in response")
	}
	if labels["env"] != "prod" {
		t.Errorf("env=%v, want prod", labels["env"])
	}
}

func TestHandleGetConfigLabels_NotFound(t *testing.T) {
	h := newTestHandlers(
		t,
		withWriteClient(&mockWriteClient{}),
		withOpsLevel(config.OpsConfiguration),
	)

	req := httptest.NewRequest("GET", "/configs/missing/labels", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetConfigLabels(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandlePatchConfigLabels_JSONPatch(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{
		ID: "cfg1",
		Spec: swarm.ConfigSpec{
			Annotations: swarm.Annotations{
				Name:   "my-config",
				Labels: map[string]string{"existing": "value"},
			},
		},
	})

	wc := &mockWriteClient{
		mockConfigWriter: mockConfigWriter{
			updateConfigLabelsFn: func(_ context.Context, id string, labels map[string]string) (swarm.Config, error) {
				return swarm.Config{
					ID:   id,
					Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Labels: labels}},
				}, nil
			},
		},
	}
	h := newTestHandlers(
		t,
		withCache(c),
		withWriteClient(wc),
		withOpsLevel(config.OpsConfiguration),
	)

	body := `[{"op":"add","path":"/new","value":"label"}]`
	req := httptest.NewRequest("PATCH", "/configs/cfg1/labels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json-patch+json")
	req.SetPathValue("id", "cfg1")
	w := httptest.NewRecorder()
	h.HandlePatchConfigLabels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "ConfigLabels" {
		t.Errorf("@type=%v, want ConfigLabels", resp["@type"])
	}
	if id, ok := resp["@id"].(string); !ok || !strings.HasSuffix(id, "/configs/cfg1/labels") {
		t.Errorf("expected @id ending in /configs/cfg1/labels, got %v", resp["@id"])
	}
	if ctx, ok := resp["@context"].(string); !ok || !strings.HasSuffix(ctx, "/api/context.jsonld") {
		t.Errorf("expected @context ending in /api/context.jsonld, got %v", resp["@context"])
	}
}

func TestHandlePatchConfigLabels_MergePatch(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{
		ID: "cfg1",
		Spec: swarm.ConfigSpec{
			Annotations: swarm.Annotations{
				Name:   "my-config",
				Labels: map[string]string{"existing": "value", "remove": "me"},
			},
		},
	})

	wc := &mockWriteClient{
		mockConfigWriter: mockConfigWriter{
			updateConfigLabelsFn: func(_ context.Context, id string, labels map[string]string) (swarm.Config, error) {
				return swarm.Config{
					ID:   id,
					Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Labels: labels}},
				}, nil
			},
		},
	}
	h := newTestHandlers(
		t,
		withCache(c),
		withWriteClient(wc),
		withOpsLevel(config.OpsConfiguration),
	)

	body := `{"new":"label","remove":null}`
	req := httptest.NewRequest("PATCH", "/configs/cfg1/labels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "cfg1")
	w := httptest.NewRecorder()
	h.HandlePatchConfigLabels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandlePatchConfigLabels_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{ID: "cfg1"})
	h := newTestHandlers(
		t,
		withCache(c),
		withWriteClient(&mockWriteClient{}),
		withOpsLevel(config.OpsConfiguration),
	)

	req := httptest.NewRequest("PATCH", "/configs/cfg1/labels", strings.NewReader(`[]`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "cfg1")
	w := httptest.NewRecorder()
	h.HandlePatchConfigLabels(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}

func TestHandlePatchConfigLabels_VersionConflict(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{
		ID:   "cfg1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "my-config"}},
	})

	wc := &mockWriteClient{
		mockConfigWriter: mockConfigWriter{
			updateConfigLabelsFn: func(_ context.Context, id string, labels map[string]string) (swarm.Config, error) {
				return swarm.Config{}, errdefs.Conflict(fmt.Errorf("version conflict"))
			},
		},
	}
	h := newTestHandlers(
		t,
		withCache(c),
		withWriteClient(wc),
		withOpsLevel(config.OpsConfiguration),
	)

	body := `[{"op":"add","path":"/new","value":"label"}]`
	req := httptest.NewRequest("PATCH", "/configs/cfg1/labels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json-patch+json")
	req.SetPathValue("id", "cfg1")
	w := httptest.NewRecorder()
	h.HandlePatchConfigLabels(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status=%d, want 409; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetSecretLabels(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{
		ID: "sec1",
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{
				Name:   "my-secret",
				Labels: map[string]string{"env": "prod"},
			},
		},
	})
	h := newTestHandlers(
		t,
		withCache(c),
		withWriteClient(&mockWriteClient{}),
		withOpsLevel(config.OpsConfiguration),
	)

	req := httptest.NewRequest("GET", "/secrets/sec1/labels", nil)
	req.SetPathValue("id", "sec1")
	w := httptest.NewRecorder()
	h.HandleGetSecretLabels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "SecretLabels" {
		t.Errorf("@type=%v, want SecretLabels", resp["@type"])
	}
}

func TestHandlePatchSecretLabels_JSONPatch(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{
		ID: "sec1",
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{
				Name:   "my-secret",
				Labels: map[string]string{"existing": "value"},
			},
		},
	})

	wc := &mockWriteClient{
		mockSecretWriter: mockSecretWriter{
			updateSecretLabelsFn: func(_ context.Context, id string, labels map[string]string) (swarm.Secret, error) {
				return swarm.Secret{
					ID:   id,
					Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Labels: labels}},
				}, nil
			},
		},
	}
	h := newTestHandlers(
		t,
		withCache(c),
		withWriteClient(wc),
		withOpsLevel(config.OpsConfiguration),
	)

	body := `[{"op":"add","path":"/new","value":"label"}]`
	req := httptest.NewRequest("PATCH", "/secrets/sec1/labels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json-patch+json")
	req.SetPathValue("id", "sec1")
	w := httptest.NewRecorder()
	h.HandlePatchSecretLabels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "SecretLabels" {
		t.Errorf("@type=%v, want SecretLabels", resp["@type"])
	}
	if id, ok := resp["@id"].(string); !ok || !strings.HasSuffix(id, "/secrets/sec1/labels") {
		t.Errorf("expected @id ending in /secrets/sec1/labels, got %v", resp["@id"])
	}
	if ctx, ok := resp["@context"].(string); !ok || !strings.HasSuffix(ctx, "/api/context.jsonld") {
		t.Errorf("expected @context ending in /api/context.jsonld, got %v", resp["@context"])
	}
}

func TestHandlePatchSecretLabels_VersionConflict(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{
		ID:   "sec1",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "my-secret"}},
	})

	wc := &mockWriteClient{
		mockSecretWriter: mockSecretWriter{
			updateSecretLabelsFn: func(_ context.Context, id string, labels map[string]string) (swarm.Secret, error) {
				return swarm.Secret{}, errdefs.Conflict(fmt.Errorf("version conflict"))
			},
		},
	}
	h := newTestHandlers(
		t,
		withCache(c),
		withWriteClient(wc),
		withOpsLevel(config.OpsConfiguration),
	)

	body := `[{"op":"add","path":"/new","value":"label"}]`
	req := httptest.NewRequest("PATCH", "/secrets/sec1/labels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json-patch+json")
	req.SetPathValue("id", "sec1")
	w := httptest.NewRecorder()
	h.HandlePatchSecretLabels(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status=%d, want 409; body: %s", w.Code, w.Body.String())
	}
}

func TestPreferMinimal_ScaleService(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{
		mockServiceLifecycleWriter: mockServiceLifecycleWriter{
			scaleServiceFn: func(_ context.Context, id string, replicas uint64) (swarm.Service, error) {
				svc := replicatedService(id)
				svc.Spec.Mode.Replicated.Replicas = &replicas
				return svc, nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"replicas":3}`
	req := httptest.NewRequest("PUT", "/services/svc1/scale", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	req.Header.Set("Prefer", "return=minimal")
	w := httptest.NewRecorder()
	h.HandleScaleService(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Preference-Applied"); got != "return=minimal" {
		t.Errorf("Preference-Applied=%q, want %q", got, "return=minimal")
	}
	if w.Body.Len() != 0 {
		t.Errorf("body should be empty, got %q", w.Body.String())
	}
}

func TestPreferMinimal_PatchServiceEnv(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Env: []string{"FOO=bar"},
				},
			},
		},
	})

	wc := &mockWriteClient{
		mockServiceSpecWriter: mockServiceSpecWriter{
			updateServiceEnvFn: func(_ context.Context, _ string, env map[string]string) (swarm.Service, error) {
				return swarm.Service{
					ID: "svc1",
					Spec: swarm.ServiceSpec{
						TaskTemplate: swarm.TaskSpec{
							ContainerSpec: &swarm.ContainerSpec{
								Env: []string{"FOO=bar", "BAZ=qux"},
							},
						},
					},
				}, nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"BAZ":"qux"}`
	req := httptest.NewRequest("PATCH", "/services/svc1/env", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.Header.Set("Prefer", "return=minimal")
	w := httptest.NewRecorder()
	h.HandlePatchServiceEnv(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Preference-Applied"); got != "return=minimal" {
		t.Errorf("Preference-Applied=%q, want %q", got, "return=minimal")
	}
}

func TestPreferMinimal_CreateConfig(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{
		mockConfigWriter: mockConfigWriter{
			createConfigFn: func(_ context.Context, _ swarm.ConfigSpec) (string, error) {
				return "cfg-new", nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"name":"myconfig","data":"aGVsbG8="}`
	req := httptest.NewRequest("POST", "/configs", strings.NewReader(body))
	req.Header.Set("Prefer", "return=minimal")
	w := httptest.NewRecorder()
	h.HandleCreateConfig(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Preference-Applied"); got != "return=minimal" {
		t.Errorf("Preference-Applied=%q, want %q", got, "return=minimal")
	}
	if got := w.Header().Get("Location"); got == "" {
		t.Error("Location header should be set")
	}
	if w.Body.Len() != 0 {
		t.Errorf("body should be empty, got %q", w.Body.String())
	}
}

func TestPreferMinimal_PatchNodeLabels(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID: "node1",
		Spec: swarm.NodeSpec{
			Annotations: swarm.Annotations{Labels: map[string]string{"env": "prod"}},
		},
	})

	wc := &mockWriteClient{
		mockNodeWriter: mockNodeWriter{
			updateNodeLabelsFn: func(_ context.Context, _ string, labels map[string]string) (swarm.Node, error) {
				return swarm.Node{
					ID: "node1",
					Spec: swarm.NodeSpec{
						Annotations: swarm.Annotations{Labels: labels},
					},
				}, nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withWriteClient(wc))

	body := `{"region":"us-east"}`
	req := httptest.NewRequest("PATCH", "/nodes/node1/labels", strings.NewReader(body))
	req.SetPathValue("id", "node1")
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.Header.Set("Prefer", "return=minimal")
	w := httptest.NewRecorder()
	h.HandlePatchNodeLabels(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Preference-Applied"); got != "return=minimal" {
		t.Errorf("Preference-Applied=%q, want %q", got, "return=minimal")
	}
}

func TestHandleGetServiceMode(t *testing.T) {
	replicas := uint64(3)
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
		},
	})

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/services/svc1/mode", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceMode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["mode"] != "replicated" {
		t.Errorf("mode=%v, want replicated", resp["mode"])
	}

	allow := w.Header().Get("Allow")
	if allow != "GET, HEAD, PUT" {
		t.Errorf("Allow=%q, want %q", allow, "GET, HEAD, PUT")
	}
}

func TestHandleGetServiceMode_Global(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Global: &swarm.GlobalService{}},
		},
	})

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/services/svc1/mode", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceMode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["mode"] != "global" {
		t.Errorf("mode=%v, want global", resp["mode"])
	}
	if resp["replicas"] != nil {
		t.Errorf("replicas=%v, want nil", resp["replicas"])
	}
}

func TestHandleGetServiceMode_ReadOnly(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{}},
		},
	})

	h := newTestHandlers(t, withCache(c), withOpsLevel(config.OpsReadOnly))
	req := httptest.NewRequest("GET", "/services/svc1/mode", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceMode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	allow := w.Header().Get("Allow")
	if allow != "GET, HEAD" {
		t.Errorf("Allow=%q, want %q", allow, "GET, HEAD")
	}
}

func TestHandleGetServiceEndpointMode(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			EndpointSpec: &swarm.EndpointSpec{
				Mode: swarm.ResolutionModeVIP,
			},
		},
	})

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/services/svc1/endpoint-mode", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceEndpointMode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["endpointMode"] != "vip" {
		t.Errorf("endpointMode=%v, want vip", resp["endpointMode"])
	}

	allow := w.Header().Get("Allow")
	if allow != "GET, HEAD, PUT" {
		t.Errorf("Allow=%q, want %q", allow, "GET, HEAD, PUT")
	}
}

func TestHandleGetServiceEndpointMode_NilEndpointSpec(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/services/svc1/endpoint-mode", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceEndpointMode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["endpointMode"] != "" {
		t.Errorf("endpointMode=%v, want empty string", resp["endpointMode"])
	}
}

func TestHandleGetServiceEndpointMode_ReadOnly(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			EndpointSpec: &swarm.EndpointSpec{
				Mode: swarm.ResolutionModeDNSRR,
			},
		},
	})

	h := newTestHandlers(t, withCache(c), withOpsLevel(config.OpsReadOnly))
	req := httptest.NewRequest("GET", "/services/svc1/endpoint-mode", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceEndpointMode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	allow := w.Header().Get("Allow")
	if allow != "GET, HEAD" {
		t.Errorf("Allow=%q, want %q", allow, "GET, HEAD")
	}
}
