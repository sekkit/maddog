package skillopt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// LoadDataset loads an explicit train/validation/test manifest. JSON maps
// directly to Dataset; TOML accepts [[train]], [[validation]], and [[test]]
// tables. Case IDs must be globally unique across all three splits.
func LoadDataset(path string) (Dataset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Dataset{}, err
	}
	var dataset Dataset
	switch strings.ToLower(filepath.Ext(path)) {
	case ".toml":
		var manifest tomlDataset
		if err := toml.Unmarshal(data, &manifest); err != nil {
			return Dataset{}, fmt.Errorf("decode skillopt TOML manifest: %w", err)
		}
		dataset, err = manifest.dataset()
		if err != nil {
			return Dataset{}, err
		}
	default:
		if err := json.Unmarshal(data, &dataset); err != nil {
			return Dataset{}, fmt.Errorf("decode skillopt JSON manifest: %w", err)
		}
	}
	if err := ValidateDataset(dataset); err != nil {
		return Dataset{}, err
	}
	return cloneDataset(dataset), nil
}

type tomlDataset struct {
	ID         string            `toml:"id"`
	Train      []tomlDatasetCase `toml:"train"`
	Validation []tomlDatasetCase `toml:"validation"`
	Test       []tomlDatasetCase `toml:"test"`
}

type tomlDatasetCase struct {
	ID       string            `toml:"id"`
	Input    string            `toml:"input"`
	Expected any               `toml:"expected"`
	Metadata map[string]string `toml:"metadata"`
}

func (m tomlDataset) dataset() (Dataset, error) {
	convert := func(split string, values []tomlDatasetCase) ([]Case, error) {
		out := make([]Case, 0, len(values))
		for i, value := range values {
			var expected json.RawMessage
			if value.Expected != nil {
				encoded, err := json.Marshal(value.Expected)
				if err != nil {
					return nil, fmt.Errorf("encode %s case %d expected value: %w", split, i+1, err)
				}
				expected = encoded
			}
			out = append(out, Case{ID: value.ID, Input: value.Input, Expected: expected, Metadata: cloneStringMap(value.Metadata)})
		}
		return out, nil
	}
	train, err := convert("train", m.Train)
	if err != nil {
		return Dataset{}, err
	}
	validation, err := convert("validation", m.Validation)
	if err != nil {
		return Dataset{}, err
	}
	test, err := convert("test", m.Test)
	if err != nil {
		return Dataset{}, err
	}
	return Dataset{ID: m.ID, Train: train, Validation: validation, Test: test}, nil
}

// ValidateDataset proves the held-out split boundary before a run starts.
func ValidateDataset(dataset Dataset) error {
	if strings.TrimSpace(dataset.ID) == "" {
		return fmt.Errorf("%w: dataset id is required", ErrInvalidDataset)
	}
	if len(dataset.Train) == 0 || len(dataset.Validation) == 0 || len(dataset.Test) == 0 {
		return fmt.Errorf("%w: train, validation, and test splits must all be non-empty", ErrInvalidDataset)
	}
	seen := make(map[string]Phase, len(dataset.Train)+len(dataset.Validation)+len(dataset.Test))
	validate := func(phase Phase, cases []Case) error {
		for i, c := range cases {
			id := strings.TrimSpace(c.ID)
			if id == "" {
				return fmt.Errorf("%w: %s case %d has no id", ErrInvalidDataset, phase, i+1)
			}
			if strings.TrimSpace(c.Input) == "" {
				return fmt.Errorf("%w: %s case %q has no input", ErrInvalidDataset, phase, id)
			}
			if previous, ok := seen[id]; ok {
				return fmt.Errorf("%w: case id %q overlaps %s and %s splits", ErrInvalidDataset, id, previous, phase)
			}
			if len(c.Expected) > 0 && !json.Valid(c.Expected) {
				return fmt.Errorf("%w: %s case %q has invalid expected JSON", ErrInvalidDataset, phase, id)
			}
			seen[id] = phase
		}
		return nil
	}
	if err := validate(PhaseTrain, dataset.Train); err != nil {
		return err
	}
	if err := validate(PhaseValidation, dataset.Validation); err != nil {
		return err
	}
	return validate(PhaseTest, dataset.Test)
}

func datasetDigest(dataset Dataset) (string, error) {
	data, err := json.Marshal(dataset)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func cloneDataset(value Dataset) Dataset {
	value.Train = cloneCases(value.Train)
	value.Validation = cloneCases(value.Validation)
	value.Test = cloneCases(value.Test)
	return value
}

func cloneCases(values []Case) []Case {
	out := make([]Case, len(values))
	for i, value := range values {
		out[i] = value
		out[i].Expected = append(json.RawMessage(nil), value.Expected...)
		out[i].Metadata = cloneStringMap(value.Metadata)
	}
	return out
}

func cloneStringMap(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]string, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}
