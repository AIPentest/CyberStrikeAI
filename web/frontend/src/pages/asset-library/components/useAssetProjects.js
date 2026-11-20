/** 项目下拉数据：资产绑定/筛选共用，失败时降级为空列表。 */
import { api } from '../../../lib/http';
import { useResource } from '../../../components/hooks';

export function useAssetProjects() {
    const { data, loading, reload } = useResource(
        () => api.get('/projects', { limit: 200 }),
        [],
        { initial: { projects: [] } }
    );
    return {
        projects: Array.isArray(data?.projects) ? data.projects : [],
        loading,
        reload
    };
}

export async function fetchAssetProjects() {
    const payload = await api.get('/projects', { limit: 200 });
    return Array.isArray(payload?.projects) ? payload.projects : [];
}
