package cacheusage

type Cache struct {
	Enabled    *bool  `json:"enabled,omitempty"`
	Path       string `json:"path,omitempty"`
	MaxBytes   int64  `json:"maxBytes,omitempty"`
	UsedBytes  *int64 `json:"usedBytes,omitempty"`
	ObservedAt string `json:"observedAt,omitempty"`
	Error      string `json:"error,omitempty"`
}

type Node struct {
	Name  string `json:"name"`
	Index Cache  `json:"index"`
	Drill Cache  `json:"drill"`
}

func PreservePreviousUsage(previous, current []Node) []Node {
	byName := make(map[string]Node, len(previous))
	for _, node := range previous {
		byName[node.Name] = node
	}
	for index := range current {
		old, exists := byName[current[index].Name]
		if !exists {
			continue
		}
		current[index].Index = preserveCache(old.Index, current[index].Index)
		current[index].Drill = preserveCache(old.Drill, current[index].Drill)
	}
	return current
}

func preserveCache(previous, current Cache) Cache {
	if current.Error == "" || current.UsedBytes != nil {
		return current
	}
	if current.Path == "" {
		errorMessage := current.Error
		current = previous
		current.Error = errorMessage
		return current
	}
	if current.Path == previous.Path {
		current.UsedBytes, current.ObservedAt = previous.UsedBytes, previous.ObservedAt
	}
	return current
}
