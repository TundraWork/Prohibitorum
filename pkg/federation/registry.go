package federation

import (
	"errors"
	"fmt"
	"regexp"
	"sync"
)

var (
	ErrUnknownProtocol = errors.New("federation: unknown protocol")
	protocolNameRE     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
)

type registryEntry struct {
	definition Definition
	descriptor Descriptor
	adapter    Adapter
}

type Registry struct {
	mu      sync.RWMutex
	entries map[string]registryEntry
}

func NewRegistry() *Registry {
	return &Registry{entries: make(map[string]registryEntry)}
}

func (r *Registry) RegisterDefinition(definition Definition) error {
	if definition == nil {
		return errors.New("federation: nil definition")
	}
	protocol := definition.Protocol()
	if !protocolNameRE.MatchString(protocol) {
		return fmt.Errorf("federation: invalid protocol %q", protocol)
	}
	descriptor := definition.Descriptor()
	if err := validateDescriptor(protocol, descriptor); err != nil {
		return err
	}
	descriptor = cloneDescriptor(descriptor)

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[protocol]; exists {
		return fmt.Errorf("federation: duplicate definition for %q", protocol)
	}
	r.entries[protocol] = registryEntry{definition: definition, descriptor: descriptor}
	return nil
}

func (r *Registry) RegisterAdapter(adapter Adapter) error {
	if adapter == nil {
		return errors.New("federation: nil adapter")
	}
	protocol := adapter.Protocol()
	if !protocolNameRE.MatchString(protocol) {
		return fmt.Errorf("federation: invalid protocol %q", protocol)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	entry, exists := r.entries[protocol]
	if !exists {
		return fmt.Errorf("%w: %s", ErrUnknownProtocol, protocol)
	}
	if entry.adapter != nil {
		return fmt.Errorf("federation: duplicate adapter for %q", protocol)
	}
	if entry.definition.Protocol() != protocol || entry.descriptor.Protocol != protocol {
		return fmt.Errorf("federation: protocol mismatch for %q", protocol)
	}
	entry.adapter = adapter
	r.entries[protocol] = entry
	return nil
}

func (r *Registry) Definition(protocol string) (Definition, error) {
	r.mu.RLock()
	entry, exists := r.entries[protocol]
	r.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrUnknownProtocol, protocol)
	}
	return entry.definition, nil
}

func (r *Registry) Adapter(protocol string) (Adapter, error) {
	r.mu.RLock()
	entry, exists := r.entries[protocol]
	r.mu.RUnlock()
	if !exists || entry.adapter == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownProtocol, protocol)
	}
	return entry.adapter, nil
}

func (r *Registry) Descriptor(protocol string) (Descriptor, error) {
	r.mu.RLock()
	entry, exists := r.entries[protocol]
	r.mu.RUnlock()
	if !exists {
		return Descriptor{}, fmt.Errorf("%w: %s", ErrUnknownProtocol, protocol)
	}
	return cloneDescriptor(entry.descriptor), nil
}

func validateDescriptor(protocol string, descriptor Descriptor) error {
	if descriptor.Protocol == "" || descriptor.Protocol != protocol {
		return fmt.Errorf("federation: descriptor protocol mismatch for %q", protocol)
	}
	fields := make(map[string]struct{}, len(descriptor.SearchFields))
	for _, field := range descriptor.SearchFields {
		if field.Key == "" {
			return errors.New("federation: empty search field key")
		}
		if _, duplicate := fields[field.Key]; duplicate {
			return fmt.Errorf("federation: duplicate search field %q", field.Key)
		}
		fields[field.Key] = struct{}{}
		if len(field.Operators) == 0 {
			return fmt.Errorf("federation: search field %q has no operators", field.Key)
		}
		operators := make(map[SearchOperator]struct{}, len(field.Operators))
		for _, operator := range field.Operators {
			switch operator {
			case SearchExact, SearchPrefix, SearchContains:
			default:
				return fmt.Errorf("federation: unknown search operator %q", operator)
			}
			if _, duplicate := operators[operator]; duplicate {
				return fmt.Errorf("federation: duplicate search operator %q", operator)
			}
			operators[operator] = struct{}{}
		}
	}
	return nil
}

func cloneDescriptor(descriptor Descriptor) Descriptor {
	clone := descriptor
	clone.SearchFields = make([]SearchField, len(descriptor.SearchFields))
	for i, field := range descriptor.SearchFields {
		clone.SearchFields[i] = field
		clone.SearchFields[i].Operators = append([]SearchOperator(nil), field.Operators...)
	}
	return clone
}
