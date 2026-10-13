package platform

import (
	"bytes"
	"encoding/json"
	"io"
)

const MaxBatch = 50

type BatchItem struct {
	RequestID string `json:"request_id"`
	Input     Input  `json:"input"`
}
type BatchError struct {
	Index int    `json:"index"`
	Code  string `json:"code"`
}

func (e *BatchError) Error() string { return e.Code }

// prepareBatch runs inside the Store transaction for execution. Empty names reuse
// the original name so a retry across a minute boundary remains idempotent.
func prepareBatch(st State, owner string, items []BatchItem) ([]BatchItem, error) {
	if len(items) < 1 || len(items) > MaxBatch {
		return nil, fail("BATCH_SIZE_1_TO_50")
	}
	out := make([]BatchItem, len(items))
	seen := map[string]bool{}
	for i, item := range items {
		if !validID.MatchString(item.RequestID) {
			return nil, &BatchError{i, "INVALID_REQUEST_ID"}
		}
		if seen[item.RequestID] {
			return nil, &BatchError{i, "DUPLICATE_REQUEST_ID"}
		}
		seen[item.RequestID] = true
		if item.Input.Name == "" {
			for _, j := range st.Jobs {
				if j.Owner == owner && j.RequestID == item.RequestID {
					item.Input.Name = j.Input.Name
					break
				}
			}
		}
		normalized, err := Normalize(item.Input)
		if err != nil {
			return nil, &BatchError{i, safeCode(err)}
		}
		item.Input = normalized
		out[i] = item
	}
	return out, nil
}

// EnqueueBatch is all-or-none locally. Remote execution remains asynchronous;
// one remote failure does not undo other jobs or cause an unsafe automatic retry.
func (e *Engine) EnqueueBatch(owner string, items []BatchItem) ([]Job, error) {
	var out []Job
	err := e.Store.Update(func(st *State) error {
		u, ok := st.Users[owner]
		if !ok || u.Disabled {
			return fail("FORBIDDEN")
		}
		prepared, err := prepareBatch(*st, owner, items)
		if err != nil {
			return err
		}
		active, total := 0, 0
		existing := map[string]Job{}
		for _, j := range st.Jobs {
			if j.Owner == owner {
				existing[j.RequestID] = j
			}
			if !terminal(j.Stage) {
				total++
				if j.Owner == owner {
					active++
				}
			}
		}
		for i, item := range prepared {
			h := Hash(item.Input)
			if j, ok := existing[item.RequestID]; ok {
				if j.Hash != h {
					return &BatchError{i, "IDEMPOTENCY_CONFLICT"}
				}
				out = append(out, j)
				continue
			}
			if total >= 1000 || active >= 50 {
				return &BatchError{i, "QUEUE_FULL"}
			}
			now := stamp()
			j := Job{ID: ID(), Owner: owner, RequestID: item.RequestID, Input: item.Input, Hash: h, Stage: "queued", CreatedAt: now, UpdatedAt: now}
			st.Jobs[j.ID] = j
			st.log(owner, "job.enqueue", j.ID)
			out = append(out, j)
			total++
			active++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type ImportFile struct {
	Name    string          `json:"name"`
	Content json.RawMessage `json:"content"`
}
type ImportedInput struct {
	Source string `json:"source"`
	Index  int    `json:"index"`
	Input  Input  `json:"input"`
}
type ImportIssue struct {
	Source string `json:"source"`
	Index  int    `json:"index"`
	Code   string `json:"code"`
}

func strictJSON(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fail("UNSUPPORTED_OR_INVALID_JSON")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fail("UNSUPPORTED_OR_INVALID_JSON")
	}
	return nil
}

// ParseImport supports local Input and the protein-only AlphaFold Server dialect.
// Unsupported fields are rejected, never silently dropped or submitted.
func ParseImport(raw json.RawMessage) (Input, error) {
	var shape map[string]json.RawMessage
	if json.Unmarshal(raw, &shape) != nil || shape == nil {
		return Input{}, fail("INVALID_JOB_JSON")
	}
	if _, ok := shape["modelSeeds"]; ok {
		return parseServerInput(raw)
	}
	if _, ok := shape["dialect"]; ok {
		return parseServerInput(raw)
	}
	var in Input
	if err := strictJSON(raw, &in); err != nil {
		return in, err
	}
	return Normalize(in)
}
func parseServerInput(raw json.RawMessage) (Input, error) {
	var body struct {
		Name      string   `json:"name"`
		Seeds     []string `json:"modelSeeds"`
		Dialect   string   `json:"dialect"`
		Version   int      `json:"version"`
		Sequences []struct {
			Protein *struct {
				Sequence string `json:"sequence"`
				Count    int    `json:"count"`
			} `json:"proteinChain"`
		} `json:"sequences"`
	}
	if err := strictJSON(raw, &body); err != nil {
		return Input{}, err
	}
	if body.Dialect != "" && body.Dialect != "alphafoldserver" {
		return Input{}, fail("UNSUPPORTED_DIALECT")
	}
	if body.Version != 0 && body.Version != 1 {
		return Input{}, fail("UNSUPPORTED_DIALECT_VERSION")
	}
	if len(body.Seeds) > 0 {
		return Input{}, fail("MANUAL_SEED_NOT_SUPPORTED")
	}
	in := Input{Name: body.Name}
	for _, s := range body.Sequences {
		if s.Protein == nil {
			return Input{}, fail("ONLY_PROTEIN_SUPPORTED")
		}
		if s.Protein.Count < 1 {
			return Input{}, fail("INVALID_COPIES_OR_SIZE")
		}
		in.Sequences = append(in.Sequences, Sequence{Sequence: s.Protein.Sequence, Copies: s.Protein.Count})
	}
	return Normalize(in)
}
func ImportInputs(files []ImportFile) ([]ImportedInput, []ImportIssue) {
	var items []ImportedInput
	issues := []ImportIssue{}
	if len(files) < 1 || len(files) > MaxBatch {
		return nil, []ImportIssue{{Code: "FILE_COUNT_1_TO_50"}}
	}
	count := 0
	for _, f := range files {
		if len(f.Name) > 200 {
			return nil, []ImportIssue{{Code: "INVALID_FILENAME"}}
		}
		raw := bytes.TrimSpace(f.Content)
		rows := []json.RawMessage{raw}
		if len(raw) > 0 && raw[0] == '[' {
			if json.Unmarshal(raw, &rows) != nil {
				issues = append(issues, ImportIssue{f.Name, 0, "INVALID_JSON"})
				continue
			}
		}
		if len(rows) == 0 {
			issues = append(issues, ImportIssue{f.Name, 0, "EMPTY_JOB_LIST"})
		}
		count += len(rows)
		if count > MaxBatch {
			return nil, []ImportIssue{{f.Name, 0, "BATCH_SIZE_1_TO_50"}}
		}
		for i, r := range rows {
			in, err := ParseImport(r)
			if err != nil {
				issues = append(issues, ImportIssue{f.Name, i, safeCode(err)})
			} else {
				items = append(items, ImportedInput{f.Name, i, in})
			}
		}
	}
	if len(issues) > 0 {
		return nil, issues
	}
	return items, issues
}
