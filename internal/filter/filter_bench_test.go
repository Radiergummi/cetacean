package filter

import (
	"fmt"
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

func BenchmarkCompile(b *testing.B) {
	b.Run("simple", func(b *testing.B) {
		for b.Loop() {
			// Reset cache to measure cold compile.
			compileCache = newProgramCache(compileCacheSize)
			Compile(`name == "test"`)
		}
	})
	b.Run("complex", func(b *testing.B) {
		for b.Loop() {
			compileCache = newProgramCache(compileCacheSize)
			Compile(`state == "running" and (role == "worker" or availability == "active")`)
		}
	})
	b.Run("cached_simple", func(b *testing.B) {
		compileCache = newProgramCache(compileCacheSize)
		Compile(`name == "test"`) // warm the cache
		for b.Loop() {
			Compile(`name == "test"`)
		}
	})
	b.Run("cached_complex", func(b *testing.B) {
		compileCache = newProgramCache(compileCacheSize)
		Compile(`state == "running" and (role == "worker" or availability == "active")`)
		for b.Loop() {
			Compile(`state == "running" and (role == "worker" or availability == "active")`)
		}
	})
}

func BenchmarkEvaluate(b *testing.B) {
	prog, _ := Compile(`state == "running" and role == "worker"`)
	b.Run("hit", func(b *testing.B) {
		env := map[string]any{
			"id": "n1", "name": "node-1", "state": "running",
			"role": "worker", "availability": "active",
		}
		for b.Loop() {
			Evaluate(prog, env)
		}
	})
	b.Run("miss", func(b *testing.B) {
		env := map[string]any{
			"id": "n1", "name": "node-1", "state": "ready",
			"role": "manager", "availability": "active",
		}
		for b.Loop() {
			Evaluate(prog, env)
		}
	})
}

func BenchmarkServiceEnv(b *testing.B) {
	replicas := uint64(3)
	svc := swarm.Service{
		ID: "svc-1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "mystack_web",
				Labels: map[string]string{"com.docker.stack.namespace": "mystack"},
			},
			Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "registry.example.com/web:v2.1.0"},
			},
		},
	}
	for b.Loop() {
		ServiceEnv(svc, nil)
	}
}

func BenchmarkFilterPipeline(b *testing.B) {
	prog, _ := Compile(`state == "running"`)
	tasks := make([]swarm.Task, 1000)
	for i := range tasks {
		state := swarm.TaskStateRunning
		if i%3 == 0 {
			state = swarm.TaskStateFailed
		}
		tasks[i] = swarm.Task{
			ID:     fmt.Sprintf("t-%d", i),
			Status: swarm.TaskStatus{State: state},
			Spec:   swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "img"}},
		}
	}

	b.Run("new_map", func(b *testing.B) {
		for b.Loop() {
			for i := range tasks {
				env := TaskEnv(tasks[i], nil)
				Evaluate(prog, env)
			}
		}
	})
	b.Run("reuse_map", func(b *testing.B) {
		for b.Loop() {
			var m map[string]any
			for i := range tasks {
				m = TaskEnv(tasks[i], m)
				Evaluate(prog, m)
			}
		}
	})
}
