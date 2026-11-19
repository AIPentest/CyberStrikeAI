/** 外观：主题（system/light/dark，useTheme）与界面语言（useI18n.setLanguage）。 */
import { RadioGroup, Radio, Select, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { useTheme } from '../../../lib/theme';
import { LANGUAGES, useI18n, useT } from '../../../lib/i18n';
import { SectionCard } from '../../../components/ui';
import { Hint } from './common';

const { Text } = Typography;

export default function AppearanceSection() {
    const t = useT();
    const { preference, theme, setPreference } = useTheme();
    const { lang, setLanguage } = useI18n();

    return (
        <>
            <SectionCard
                title={t('theme.titleSystem', null, '外观主题')}
                description={t('settings.appearanceThemeDesc', null, '主题与语言只保存在当前浏览器（localStorage），不会写入服务端配置。')}
            >
                <RadioGroup
                    value={preference}
                    onChange={event => setPreference(event.target.value)}
                    direction="horizontal"
                >
                    <Radio value="system">{t('theme.system', null, '跟随系统')}</Radio>
                    <Radio value="light">{t('theme.light', null, '浅色')}</Radio>
                    <Radio value="dark">{t('theme.dark', null, '暗色')}</Radio>
                </RadioGroup>
                <Space spacing={8} className="settings-appearance-status">
                    <Tag size="small" color="blue">
                        {t('settings.currentTheme', null, '当前生效')}：{theme === 'dark' ? t('theme.dark', null, '暗色') : t('theme.light', null, '浅色')}
                    </Tag>
                    <Hint>{`preference = ${preference}`}</Hint>
                </Space>
            </SectionCard>

            <SectionCard
                title={t('settings.languageTitle', null, '界面语言')}
                description={t('settings.languageDesc', null, '切换到独立语言包；缺失词条回落到中文。')}
            >
                <Space spacing={12} align="center">
                    <Select
                        value={lang}
                        onChange={value => setLanguage(value)}
                        optionList={LANGUAGES.map(item => ({ value: item.id, label: item.label }))}
                        style={{ width: 220 }}
                    />
                    <Text type="tertiary">{lang}</Text>
                </Space>
            </SectionCard>
        </>
    );
}
