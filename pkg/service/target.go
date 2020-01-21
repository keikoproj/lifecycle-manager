package service

type TargetType string

func (t TargetType) String() string {
	return string(t)
}

const (
	// TargetTypeClassicELB defines a target type of classic elb
	TargetTypeClassicELB TargetType = "classic-elb"
	// TargetTypeTargetGroup defines a target type of target-group
	TargetTypeTargetGroup TargetType = "target-group"
)

// Target defines a deregistration target
type Target struct {
	Type       TargetType
	TargetId   string
	InstanceId string
	Port       int64
}

func (m *Manager) NewTarget(targetId, instanceId string, port int64, targetType TargetType) *Target {
	return &Target{
		TargetId:   targetId,
		InstanceId: instanceId,
		Port:       port,
		Type:       targetType,
	}
}

// LoadTargets loads a list of targets by key
func (m *Manager) LoadTargets(key interface{}) []*Target {
	targets := []*Target{}
	if val, ok := m.targets.Load(key); ok {
		return val.([]*Target)
	}
	m.SetTargets(key, targets)
	return targets
}

// SetTargets sets targets to a specific key
func (m *Manager) SetTargets(key interface{}, targets []*Target) {
	m.targets.Store(key, targets)
}

// GetTargetInstanceIds gets instance ids for a specific key
func (m *Manager) GetTargetInstanceIds(key interface{}) []string {
	list := []string{}
	for _, target := range m.LoadTargets(key) {
		list = append(list, target.InstanceId)
	}
	return list
}

// GetTargetMapping gets instanceID>port mapping for a specific key
func (m *Manager) GetTargetMapping(key interface{}) map[string]int64 {
	mapping := map[string]int64{}
	for _, target := range m.LoadTargets(key) {
		mapping[target.InstanceId] = target.Port
	}
	return mapping
}

// AddTargetByInstance adds a target by it's instance ID
func (m *Manager) AddTargetByInstance(key interface{}, add *Target) {
	targets := m.LoadTargets(key)
	newTargets := []*Target{}
	conflict := false
	// TODO: Implement this better
	for _, target := range targets {
		if target.InstanceId == add.InstanceId {
			conflict = true
			newTargets = append(newTargets, add)
		} else {
			newTargets = append(newTargets, target)
		}
	}
	if len(targets) == 0 || !conflict {
		newTargets = append(newTargets, add)
	}
	m.SetTargets(key, newTargets)
}

// RemoveTargetByInstance removes a target by it's instance ID
func (m *Manager) RemoveTargetByInstance(key interface{}, instanceID string) {
	targets := m.LoadTargets(key)
	newTargets := []*Target{}
	for _, target := range targets {
		if target.InstanceId != instanceID {
			newTargets = append(newTargets, target)
		}
	}
	m.SetTargets(key, newTargets)
}
