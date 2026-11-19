/** 深度测评路由入口：列表 / 详情深链接（?id= 与 ?tab=）。 */
import { useCallback } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Empty } from '@douyinfe/semi-ui';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import AssessmentList from './List';
import Workbench from './Workbench';
import { PERM_READ } from './labels';
import './page.css';

export default function DeepAssessmentsPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const [params, setParams] = useSearchParams();
    const id = params.get('id') || params.get('assessment_id') || '';
    const requestedTab = params.get('tab') || 'overview';
    const tab = requestedTab === 'evidence-path' ? 'evidence' : requestedTab;

    const openTask = useCallback(taskId => {
        const next = new URLSearchParams();
        next.set('id', taskId);
        next.set('tab', 'overview');
        setParams(next);
    }, [setParams]);

    const changeTab = useCallback(nextTab => {
        const next = new URLSearchParams();
        next.set('id', id);
        next.set('tab', nextTab);
        setParams(next, { replace: true });
    }, [id, setParams]);

    const backToList = useCallback(() => {
        setParams(new URLSearchParams(), { replace: false });
    }, [setParams]);

    if (!hasPermission(PERM_READ)) {
        return (
            <div className="assessment-page">
                <Empty
                    title={t('common.forbidden', null, '当前账号没有访问该页面的权限')}
                    description="深度测评需要 assessments:read 权限，请联系管理员分配。"
                    style={{ padding: 64 }}
                />
            </div>
        );
    }

    if (id) {
        return (
            <Workbench
                key={id}
                id={id}
                tab={tab}
                criterion={Number(params.get('criterion')) || 0}
                evidencePath={requestedTab === 'evidence-path'}
                evidenceStepId={params.get('step_id') || ''}
                observation={params.get('observation') || ''}
                onTabChange={changeTab}
                onBack={backToList}
                onDeleted={backToList}
            />
        );
    }

    return <AssessmentList onOpen={openTask} />;
}
