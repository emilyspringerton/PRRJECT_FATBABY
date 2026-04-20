package eventstore_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type storeFactory func(rootDir string) (eventstore.EventStore, error)

func eventStoreContract(factory storeFactory) {
	var (
		ctx     context.Context
		rootDir string
		store   eventstore.EventStore
	)

	newEvent := func(id, typ string, payload any) eventstore.Event {
		bytes, err := json.Marshal(payload)
		Expect(err).NotTo(HaveOccurred())
		return eventstore.Event{
			ID:           id,
			Type:         typ,
			OccurredAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			AggregateKey: "0000320193-25-000073",
			Source:       "sec-edgar",
			Data:         bytes,
		}
	}

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		rootDir, err = os.MkdirTemp("", "eventstore-contract-*")
		Expect(err).NotTo(HaveOccurred())
		store, err = factory(rootDir)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if store != nil {
			Expect(store.Close()).To(Succeed())
		}
		Expect(os.RemoveAll(rootDir)).To(Succeed())
	})

	It("append then read round trip", func() {
		event := newEvent("evt-1", "filing_discovered", map[string]any{"cik": "320193", "form": "10-K"})
		appended, err := store.Append(ctx, event)
		Expect(err).NotTo(HaveOccurred())
		Expect(appended).To(HaveLen(1))
		Expect(appended[0].Sequence).To(Equal(uint64(1)))
		Expect(appended[0].Event.Type).To(Equal("filing_discovered"))

		read, err := store.ReadFrom(ctx, 1, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(read).To(HaveLen(1))
		Expect(read[0]).To(Equal(appended[0]))
	})

	It("sequence numbers strictly increase", func() {
		_, err := store.Append(ctx,
			newEvent("evt-1", "filing_discovered", map[string]any{"accession": "a1"}),
			newEvent("evt-2", "filing_fetch_started", map[string]any{"accession": "a1"}),
		)
		Expect(err).NotTo(HaveOccurred())
		second, err := store.Append(ctx, newEvent("evt-3", "filing_fetched", map[string]any{"accession": "a1"}))
		Expect(err).NotTo(HaveOccurred())
		Expect(second[0].Sequence).To(Equal(uint64(3)))
	})

	It("batch append preserves order", func() {
		batch := []eventstore.Event{
			newEvent("evt-1", "filing_discovered", map[string]any{"idx": 1}),
			newEvent("evt-2", "filing_fetch_started", map[string]any{"idx": 2}),
			newEvent("evt-3", "filing_fetched", map[string]any{"idx": 3}),
		}
		recs, err := store.Append(ctx, batch...)
		Expect(err).NotTo(HaveOccurred())
		Expect(recs).To(HaveLen(3))
		for i := range recs {
			Expect(recs[i].Event.ID).To(Equal(batch[i].ID))
			Expect(recs[i].Sequence).To(Equal(uint64(i + 1)))
		}
	})

	It("read with limits works", func() {
		for i := 1; i <= 5; i++ {
			_, err := store.Append(ctx, newEvent(fmt.Sprintf("evt-%d", i), "mapping_refresh_completed", map[string]any{"iteration": i}))
			Expect(err).NotTo(HaveOccurred())
		}
		recs, err := store.ReadFrom(ctx, 2, 2)
		Expect(err).NotTo(HaveOccurred())
		Expect(recs).To(HaveLen(2))
		Expect(recs[0].Sequence).To(Equal(uint64(2)))
		Expect(recs[1].Sequence).To(Equal(uint64(3)))
	})

	It("latest sequence works for empty and non-empty stores", func() {
		latest, err := store.LatestSequence(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(latest).To(Equal(uint64(0)))

		_, err = store.Append(ctx, newEvent("evt-1", "filing_parse_failed", map[string]any{"reason": "xml malformed"}))
		Expect(err).NotTo(HaveOccurred())
		latest, err = store.LatestSequence(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(latest).To(Equal(uint64(1)))
	})

	It("persistence across close and reopen", func() {
		_, err := store.Append(ctx, newEvent("evt-1", "filing_discovered", map[string]any{"accession": "a1"}))
		Expect(err).NotTo(HaveOccurred())
		Expect(store.Close()).To(Succeed())

		store, err = factory(rootDir)
		Expect(err).NotTo(HaveOccurred())

		latest, err := store.LatestSequence(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(latest).To(Equal(uint64(1)))

		recs, err := store.ReadFrom(ctx, 1, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(recs).To(HaveLen(1))
		Expect(recs[0].Event.ID).To(Equal("evt-1"))
	})

	It("empty reads beyond tail return empty slice", func() {
		_, err := store.Append(ctx, newEvent("evt-1", "filing_discovered", map[string]any{"accession": "a1"}))
		Expect(err).NotTo(HaveOccurred())
		recs, err := store.ReadFrom(ctx, 20, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(recs).To(BeEmpty())
	})

	It("truncated trailing line recovery preserves earlier records", func() {
		_, err := store.Append(ctx,
			newEvent("evt-1", "filing_discovered", map[string]any{"accession": "a1"}),
			newEvent("evt-2", "filing_fetched", map[string]any{"accession": "a1"}),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(store.Close()).To(Succeed())

		eventFiles, err := filepath.Glob(filepath.Join(rootDir, "events", "*.ndjson"))
		Expect(err).NotTo(HaveOccurred())
		Expect(eventFiles).To(HaveLen(1))

		f, err := os.OpenFile(eventFiles[0], os.O_APPEND|os.O_WRONLY, 0)
		Expect(err).NotTo(HaveOccurred())
		_, err = f.WriteString(`{"sequence":3,"event":{"id":"evt-3"`)
		Expect(err).NotTo(HaveOccurred())
		Expect(f.Close()).To(Succeed())

		store, err = factory(rootDir)
		Expect(err).NotTo(HaveOccurred())
		recs, err := store.ReadFrom(ctx, 1, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(recs).To(HaveLen(2))
		Expect(recs[0].Sequence).To(Equal(uint64(1)))
		Expect(recs[1].Sequence).To(Equal(uint64(2)))

		latest, err := store.LatestSequence(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(latest).To(Equal(uint64(2)))
	})

	It("event payload round-trips correctly", func() {
		input := map[string]any{
			"filing_url": "https://www.sec.gov/Archives/edgar/data/320193/000032019325000073/aapl-20240928x10k.htm",
			"size":       12345,
			"metadata": map[string]any{
				"attempt": 1,
				"ok":      true,
			},
		}
		_, err := store.Append(ctx, newEvent("evt-1", "filing_fetched", input))
		Expect(err).NotTo(HaveOccurred())

		recs, err := store.ReadFrom(ctx, 1, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(recs).To(HaveLen(1))

		var got map[string]any
		Expect(json.Unmarshal(recs[0].Event.Data, &got)).To(Succeed())
		Expect(got).To(HaveKeyWithValue("filing_url", input["filing_url"]))
		Expect(got).To(HaveKey("metadata"))
	})

	It("rejects invalid input", func() {
		_, err := store.Append(ctx)
		Expect(err).To(MatchError(eventstore.ErrEmptyAppend))

		_, err = store.Append(ctx, eventstore.Event{Type: "filing_discovered", Data: json.RawMessage(`{"a":1}`)})
		Expect(err).To(MatchError(eventstore.ErrInvalidEventID))
	})
}
