import { useState } from 'react';
import { Banner, Button, Card, Form, Typography } from '@douyinfe/semi-ui';
import { IconArticle, IconBranch, IconBriefcase, IconLock, IconShield, IconUser } from '@douyinfe/semi-icons';
import { useAuth } from '../lib/auth';
import { useT } from '../lib/i18n';
import { errorMessage } from '../lib/http';

const { Title, Text, Paragraph } = Typography;

export default function LoginPage() {
    const { login } = useAuth();
    const t = useT();
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState('');

    async function submit(values) {
        setLoading(true);
        setError('');
        try {
            await login(values.username, values.password);
        } catch (err) {
            setError(errorMessage(err, t('login.loginFailed', null, '登录失败')));
        } finally {
            setLoading(false);
        }
    }

    return (
        <main className="cs-login-page">
            <section className="cs-login-story">
                <div className="cs-login-brand">
                    <img src="/static/logo.png" alt="" className="cs-login-logo" />
                    <span className="cs-brand-name">CyberStrike<span>AI</span></span>
                </div>
                <div className="cs-login-intro">
                    <span className="cs-login-emblem" aria-hidden="true"><IconShield size="extra-large" /></span>
                    <Title heading={1} className="cs-login-headline">{t('login.headline', null, '让每一次探索，都有据可循。')}</Title>
                    <Paragraph type="tertiary">{t('login.intro', null, '从目标梳理到证据归档，在一个工作台中完成你的安全研究。')}</Paragraph>
                    <div className="cs-login-features">
                        <div className="cs-login-feature"><IconBriefcase size="large" /><div><strong>{t('login.projectTitle', null, '围绕项目协作')}</strong>{t('login.projectHint', null, '跨会话共享目标、上下文与研究进展')}</div></div>
                        <div className="cs-login-feature"><IconBranch size="large" /><div><strong>{t('login.assessmentTitle', null, '让探索持续推进')}</strong>{t('login.assessmentHint', null, '规划、执行与人工复核，始终保持可控')}</div></div>
                        <div className="cs-login-feature"><IconArticle size="large" /><div><strong>{t('login.evidenceTitle', null, '以证据支撑结论')}</strong>{t('login.evidenceHint', null, '保留发现、来源与验收记录')}</div></div>
                    </div>
                </div>
                <Text type="tertiary" size="small">{t('login.platformTagline', null, 'AI 驱动的安全研究工作台')}</Text>
            </section>
            <section className="cs-login-form-panel">
                <Card className="cs-login-card" bordered={false}>
                    <Title heading={2} className="cs-login-form-heading">{t('login.welcome', null, '欢迎回来')}</Title>
                    <Text type="tertiary">{t('login.welcomeHint', null, '登录你的安全工作台，继续探索。')}</Text>
                    <Form onSubmit={submit} className="cs-login-form">
                        {error && <Banner type="danger" description={error} closeIcon={null} className="cs-error-banner" />}
                        <Form.Input
                            field="username"
                            label={t('login.username', null, '用户名')}
                            prefix={<IconUser />}
                            placeholder={t('login.usernamePlaceholder', null, '请输入用户名')}
                            size="large"
                            autoComplete="username"
                            rules={[{ required: true, message: t('login.usernameRequired', null, '请输入用户名') }]}
                        />
                        <Form.Input
                            field="password"
                            label={t('login.password', null, '密码')}
                            prefix={<IconLock />}
                            type="password"
                            size="large"
                            autoComplete="current-password"
                            placeholder={t('login.passwordPlaceholder', null, '请输入密码')}
                            rules={[{ required: true, message: t('login.passwordRequired', null, '请输入密码') }]}
                        />
                        <Button htmlType="submit" theme="solid" type="primary" size="large" block loading={loading}>
                            {t('login.submit', null, '登录')}
                        </Button>
                    </Form>
                    <Typography.Text link={{ href: '/' }}>
                        {t('header.classicWorkspace', null, '返回经典界面')}
                    </Typography.Text>
                    <Text type="tertiary" className="cs-login-note">{t('login.authorizedOnly', null, '仅在明确授权的范围内开展安全测试')}</Text>
                </Card>
            </section>
        </main>
    );
}
