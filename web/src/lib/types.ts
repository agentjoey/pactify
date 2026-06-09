export interface Seat { id: string; roles: string[] }
export interface Task { id: string; owner: string; status: string; reviewer: string; spec: string; evidence: string }
export interface Feature { id: string; branch: string; status: string; tasks: Task[] }
export interface State { project: string; agents: Seat[]; features: Feature[]; awaiting_count: number }
export interface PactEvent { event_id: string; ts: string; agent_id: string; role: string; event_type: string; task_id: string; feature: string; payload: Record<string, unknown> }
export interface ProjectMeta { id: string; name: string; path: string; project: string; feature_count: number; awaiting_count: number }
export interface BoardTask { task: Task; feature: string }
