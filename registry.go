package mailkit

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

type ProviderFieldSpec struct {
	Name        string                `json:"name"`
	Label       string                `json:"label"`
	InputType   string                `json:"input_type"`
	Placeholder string                `json:"placeholder,omitempty"`
	Required    bool                  `json:"required"`
	Options     []ProviderFieldOption `json:"options,omitempty"`
}

type ProviderFieldOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type ProviderSpec struct {
	Name        string              `json:"name"`
	DisplayName string              `json:"display_name"`
	Supported   bool                `json:"supported"`
	Note        string              `json:"note,omitempty"`
	Fields      []ProviderFieldSpec `json:"fields"`
}

type FactoryDependencies struct {
}

type Factory func(config ProviderConfig, dependencies FactoryDependencies) (Provider, error)

type Registration struct {
	Spec    ProviderSpec
	Factory Factory
}

var (
	registryMu          sync.RWMutex
	providerRegistryMap = map[string]Registration{}
)

func MustRegister(registration Registration) {
	if err := Register(registration); err != nil {
		panic(err)
	}
}

func Register(registration Registration) error {
	normalizedRegistration, err := normalizeRegistration(registration)
	if err != nil {
		return err
	}

	registryMu.Lock()
	defer registryMu.Unlock()

	if _, exists := providerRegistryMap[normalizedRegistration.Spec.Name]; exists {
		return fmt.Errorf("provider %s is already registered", normalizedRegistration.Spec.Name)
	}

	providerRegistryMap[normalizedRegistration.Spec.Name] = normalizedRegistration
	return nil
}

func ProviderSpecs() map[string]ProviderSpec {
	registryMu.RLock()
	defer registryMu.RUnlock()

	specs := make(map[string]ProviderSpec, len(providerRegistryMap))
	for providerName, registration := range providerRegistryMap {
		specs[providerName] = cloneSpec(registration.Spec)
	}
	return specs
}

func ProviderNames() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	providerNames := make([]string, 0, len(providerRegistryMap))
	for providerName := range providerRegistryMap {
		providerNames = append(providerNames, providerName)
	}
	slices.Sort(providerNames)
	return providerNames
}

func NewProvider(providerName string, config ProviderConfig, dependencies FactoryDependencies) (Provider, error) {
	registration, err := lookupRegistration(providerName)
	if err != nil {
		return nil, err
	}

	normalizedConfig := config.normalized()
	for _, fieldSpec := range registration.Spec.Fields {
		if !fieldSpec.Required {
			continue
		}
		if !normalizedConfig.HasValue(fieldSpec.Name) {
			return nil, fmt.Errorf("provider %s requires %s", registration.Spec.Name, fieldSpec.Name)
		}
	}

	provider, err := registration.Factory(normalizedConfig, dependencies)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, fmt.Errorf("provider %s factory returned nil", registration.Spec.Name)
	}
	if provider.Name() != registration.Spec.Name {
		return nil, fmt.Errorf("provider %s factory returned mismatched name %s", registration.Spec.Name, provider.Name())
	}
	return provider, nil
}

func NewProviders(providerNames []string, configs map[string]ProviderConfig, dependencies FactoryDependencies) ([]Provider, error) {
	if len(providerNames) == 0 {
		return nil, errors.New("at least one provider name is required")
	}

	providers := make([]Provider, 0, len(providerNames))
	seenProviders := make(map[string]struct{}, len(providerNames))
	for _, rawProviderName := range providerNames {
		providerName := normalizeProviderName(rawProviderName)
		if providerName == "" {
			continue
		}
		if _, seen := seenProviders[providerName]; seen {
			continue
		}
		seenProviders[providerName] = struct{}{}

		providerConfig := ProviderConfig{}
		if configs != nil {
			providerConfig = configs[providerName]
		}
		provider, err := NewProvider(providerName, providerConfig, dependencies)
		if err != nil {
			return nil, err
		}
		providers = append(providers, provider)
	}

	if len(providers) == 0 {
		return nil, errors.New("at least one valid provider is required")
	}
	return providers, nil
}

func lookupRegistration(providerName string) (Registration, error) {
	normalizedProviderName := normalizeProviderName(providerName)
	if normalizedProviderName == "" {
		return Registration{}, errors.New("provider name is required")
	}

	registryMu.RLock()
	defer registryMu.RUnlock()

	registration, ok := providerRegistryMap[normalizedProviderName]
	if !ok {
		return Registration{}, fmt.Errorf("unknown provider: %s", normalizedProviderName)
	}
	return registration, nil
}

func normalizeRegistration(registration Registration) (Registration, error) {
	spec := registration.Spec
	spec.Name = normalizeProviderName(spec.Name)
	spec.DisplayName = strings.TrimSpace(spec.DisplayName)
	if spec.Name == "" {
		return Registration{}, errors.New("provider spec name is required")
	}
	if spec.DisplayName == "" {
		return Registration{}, fmt.Errorf("provider %s display name is required", spec.Name)
	}
	if registration.Factory == nil {
		return Registration{}, fmt.Errorf("provider %s factory is required", spec.Name)
	}
	spec.Supported = true
	spec.Fields = cloneFields(spec.Fields)

	return Registration{
		Spec:    spec,
		Factory: registration.Factory,
	}, nil
}

func cloneSpec(spec ProviderSpec) ProviderSpec {
	clonedSpec := spec
	clonedSpec.Fields = cloneFields(spec.Fields)
	return clonedSpec
}

func cloneFields(fields []ProviderFieldSpec) []ProviderFieldSpec {
	if len(fields) == 0 {
		return nil
	}

	clonedFields := make([]ProviderFieldSpec, 0, len(fields))
	for _, field := range fields {
		normalizedFieldName := normalizeProviderKey(field.Name)
		if normalizedFieldName == "" {
			continue
		}
		clonedField := field
		clonedField.Name = normalizedFieldName
		clonedField.Label = strings.TrimSpace(clonedField.Label)
		clonedField.InputType = strings.TrimSpace(clonedField.InputType)
		clonedField.Placeholder = strings.TrimSpace(clonedField.Placeholder)
		clonedField.Options = cloneFieldOptions(clonedField.Options)
		clonedFields = append(clonedFields, clonedField)
	}
	return clonedFields
}

func cloneFieldOptions(options []ProviderFieldOption) []ProviderFieldOption {
	if len(options) == 0 {
		return nil
	}

	clonedOptions := make([]ProviderFieldOption, 0, len(options))
	for _, option := range options {
		value := strings.TrimSpace(option.Value)
		label := strings.TrimSpace(option.Label)
		if value == "" {
			continue
		}
		if label == "" {
			label = value
		}
		clonedOptions = append(clonedOptions, ProviderFieldOption{
			Value: value,
			Label: label,
		})
	}
	return clonedOptions
}

func normalizeProviderName(providerName string) string {
	return strings.ToLower(strings.TrimSpace(providerName))
}

func normalizeProviderKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}
