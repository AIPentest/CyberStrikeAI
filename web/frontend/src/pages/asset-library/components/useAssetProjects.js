/** 项目下拉数据：资产范围与绑定共用。 */
import { api } from '../../../lib/http';
import { useResource } from '../../../components/hooks';

export function useAssetProjects() {
    const { data, loading, error, reload } = useResource(
        fetchAssetProjects,
        []
    );
    return {
        projects: data || [],
        loading,
        error,
        reload
    };
}

export async function fetchAssetProjects() {
    const projects = [];
    const limit = 500;
    let total;
    do {
        const payload = await api.get('/projects', { limit, offset: projects.length });
        const batch = payload?.projects || [];
        projects.push(...batch);
        if (batch.length < limit) break;
        total = Number(payload?.total || projects.length);
    } while (projects.length < total);
    return projects;
}
