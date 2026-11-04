package cluster

import (
	"errors"
	"slices"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
)

func TestTimelineScopeQuery(t *testing.T) {
	c := cache.New(nil)
	reader := AnnotationCaller{CanRead: func(string) bool { return true }, AnyGrant: true}

	t.Run("no grant", func(t *testing.T) {
		_, err := TimelineScope{}.Query(c, AnnotationCaller{CanRead: reader.CanRead})
		if !errors.Is(err, ErrNoGrant) {
			t.Errorf("err = %v, want ErrNoGrant", err)
		}
	})

	t.Run("an unknown kind", func(t *testing.T) {
		if _, err := (TimelineScope{Kinds: []string{"change", "deploy"}}).Query(
			c,
			reader,
		); err == nil {
			t.Error("accepted kind deploy")
		}
	})

	t.Run("empty values are no filter", func(t *testing.T) {
		q, err := TimelineScope{
			Types: []cache.EventType{
				"",
			},
			Kinds:   []string{""},
			Tags:    []string{""},
			Authors: []string{""},
		}.Query(c, reader)
		if err != nil || q.Type != "" || len(q.Types)+len(q.Kinds)+len(q.Tags)+len(q.Authors) > 0 {
			t.Errorf("query = %+v, %v; want no filter", q, err)
		}
	})

	t.Run("one of Types names the resource", func(t *testing.T) {
		q, _ := TimelineScope{Types: []cache.EventType{cache.EventStack}, Resource: "shop"}.
			Query(c, reader)
		if q.Type != cache.EventStack || q.Types != nil || q.ResourceID != "shop" {
			t.Errorf("query = %+v, want the stack shop", q)
		}
	})

	t.Run("a service with its tasks", func(t *testing.T) {
		q, _ := TimelineScope{Type: cache.EventService, Resource: "svc1", Tasks: true}.
			Query(c, reader)
		if q.Type != "" ||
			!slices.Equal(q.Types, []cache.EventType{cache.EventService, cache.EventTask}) {
			t.Errorf("query = %+v, want services and tasks", q)
		}
	})
}
