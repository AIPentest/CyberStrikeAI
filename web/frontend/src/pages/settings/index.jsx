import { Button } from '@douyinfe/semi-ui';
import { PageHeader } from '../../components/ui';
import { useT } from '../../lib/i18n';
import ModelsSection from './sections/ModelsSection';
import './page.css';

export default function SettingsPage() {
    const t = useT();
    return (
        <div className="settings-page">
            <PageHeader
                title={t('settingsProbe.modelToolsTitle', null, '模型与工具')}
                extra={<Button href="/#settings?section=basic">{t('settingsProbe.moreSettings', null, '更多设置')}</Button>}
            />
            <ModelsSection />
        </div>
    );
}
