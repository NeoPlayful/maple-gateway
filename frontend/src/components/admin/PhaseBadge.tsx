// 部署阶段徽章：直接复用状态徽章。
import { StatusBadge } from './StatusBadge';

export function PhaseBadge({ phase }: { phase: string }) {
  return <StatusBadge value={phase} />;
}
