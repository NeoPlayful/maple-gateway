package docker

import "testing"

// CM 指定了容器名时直接沿用（Compose 风格，含项目短码/版本/序号）。
func TestResolveContainerNameUsesSpecName(t *testing.T) {
	got := resolveContainerName(CreateSpec{
		InstanceID: "9274e6e2-eef3-4ab3-9681-05bbdc9a6390",
		Name:       "maple-574aa2d4d469-v3-1",
	})
	if got != "maple-574aa2d4d469-v3-1" {
		t.Errorf("name = %q, want maple-574aa2d4d469-v3-1", got)
	}
}

// 未指定容器名时退回 maple-<实例短码>（既有行为，无项目归属版本用）。
func TestResolveContainerNameFallsBackToInstanceID(t *testing.T) {
	got := resolveContainerName(CreateSpec{InstanceID: "9274e6e2-eef3-4ab3-9681-05bbdc9a6390"})
	if got != "maple-9274e6e2eef3" {
		t.Errorf("name = %q, want maple-9274e6e2eef3", got)
	}
}

// 序号非 0 时写入 maple.replica_index 标签；为 0（未分配）时不写。
func TestContainerLabelsReplicaIndex(t *testing.T) {
	withIdx := containerLabels("maple.managed", CreateSpec{
		InstanceID: "i1", ProjectID: "p1", ReplicaIndex: 2,
	})
	if got := withIdx[LabelReplicaIndex]; got != "2" {
		t.Errorf("replica_index = %q, want 2", got)
	}
	if got := withIdx[LabelProjectID]; got != "p1" {
		t.Errorf("project_id = %q, want p1", got)
	}

	noIdx := containerLabels("maple.managed", CreateSpec{InstanceID: "i1"})
	if _, ok := noIdx[LabelReplicaIndex]; ok {
		t.Errorf("replica_index should be absent when index = 0, got %q", noIdx[LabelReplicaIndex])
	}
	if noIdx["maple.managed"] != "true" || noIdx[LabelInstanceID] != "i1" {
		t.Errorf("managed/instance labels missing: %v", noIdx)
	}
}
