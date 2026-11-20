/** 新建/编辑资产：Form + 目标自动解析（host/ip/domain/port/protocol 联动）。 */
import { useEffect, useMemo, useRef, useState } from 'react';
import { Banner, Button, Form, Space } from '@douyinfe/semi-ui';
import { api, errorMessage } from '../../../lib/http';
import { useT } from '../../../lib/i18n';
import { Drawer, toastSuccess, toastWarning } from '../../../components/ui';
import {
    CRITICALITY_OPTIONS, ENVIRONMENT_OPTIONS, STATUS_OPTIONS
} from './assetConstants';
import {
    isIPv4, isIPv6, normalizeDomain, parseAssetTarget, splitAssetTargets, targetInputFromAsset
} from './assetTarget';

const FORM_CLASS = 'asset-library-editor';

function initialValues(asset, defaultProjectId) {
    return {
        target: targetInputFromAsset(asset),
        host: asset?.host || '',
        ip: asset?.ip || '',
        domain: asset?.domain || '',
        port: asset?.port ?? undefined,
        protocol: asset?.protocol || '',
        title: asset?.title || '',
        server: asset?.server || '',
        project_id: asset ? asset.project_id || '' : defaultProjectId,
        status: asset?.status || 'active',
        environment: asset?.environment || '',
        criticality: asset?.criticality || '',
        country: asset?.country || '',
        province: asset?.province || '',
        city: asset?.city || '',
        responsible_person: asset?.responsible_person || '',
        department: asset?.department || '',
        business_system: asset?.business_system || '',
        tags: Array.isArray(asset?.tags) ? asset.tags : []
    };
}

export default function AssetEditorModal({ visible, asset, defaultProjectId = '', projects, onClose, onSaved }) {
    const t = useT();
    const [formApi, setFormApi] = useState(null);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    const parsedTarget = useRef(targetInputFromAsset(asset));

    const values = useMemo(() => initialValues(asset, defaultProjectId), [asset, defaultProjectId, visible]);
    const editing = Boolean(asset?.id);

    useEffect(() => {
        if (!visible) return;
        parsedTarget.current = targetInputFromAsset(asset);
        setError('');
        setBusy(false);
    }, [visible, asset]);

    const projectOptions = useMemo(
        () => (projects || []).map(project => ({ value: project.id, label: project.name || project.id })),
        [projects]
    );

    const applyTarget = (showError) => {
        if (!formApi) return null;
        const raw = String(formApi.getValue('target') || '').trim();
        try {
            const parsed = parseAssetTarget(raw);
            formApi.setValues({
                host: parsed.host,
                ip: parsed.ip,
                domain: parsed.domain,
                port: parsed.port || undefined,
                protocol: parsed.protocol
            });
            parsedTarget.current = raw;
            return parsed;
        } catch (parseError) {
            if (showError) setError(parseError.message);
            return null;
        }
    };

    const buildPayload = () => {
        const current = formApi.getValue();
        const rawTarget = String(current.target || '').trim();
        let payload = {
            host: String(current.host || '').trim(),
            ip: String(current.ip || '').trim().toLowerCase(),
            domain: String(current.domain || '').trim(),
            port: Number(current.port || 0),
            protocol: String(current.protocol || '').trim().toLowerCase(),
            title: String(current.title || '').trim(),
            server: String(current.server || '').trim(),
            status: current.status || 'active',
            source: asset?.source || 'manual',
            source_query: asset?.source_query || '',
            country: String(current.country || '').trim(),
            province: String(current.province || '').trim(),
            city: String(current.city || '').trim(),
            responsible_person: String(current.responsible_person || '').trim(),
            department: String(current.department || '').trim(),
            business_system: String(current.business_system || '').trim(),
            environment: current.environment || '',
            criticality: current.criticality || '',
            project_id: current.project_id || '',
            tags: Array.isArray(current.tags) ? current.tags.map(tag => String(tag).trim()).filter(Boolean) : []
        };
        if (rawTarget && rawTarget !== parsedTarget.current) {
            try {
                const parsed = parseAssetTarget(rawTarget);
                payload = {
                    ...payload,
                    host: payload.host || parsed.host,
                    ip: parsed.ip || payload.ip,
                    domain: parsed.domain || payload.domain,
                    port: parsed.port || payload.port,
                    protocol: parsed.protocol || payload.protocol
                };
            } catch (parseError) {
                throw new Error(parseError.message);
            }
        }
        return payload;
    };

    const buildCreatedAssets = () => {
        const targets = splitAssetTargets(formApi.getValue('targets'));
        if (!targets.length) throw new Error(t('assets.targetRequired', null, '请输入资产地址'));
        if (targets.length > 200) throw new Error('一次最多添加 200 个地址');
        const projectID = formApi.getValue('project_id') || '';
        return targets.map(target => {
            const parsed = parseAssetTarget(target);
            return {
                host: parsed.host || parsed.domain || parsed.ip,
                ip: parsed.ip,
                domain: parsed.domain,
                port: parsed.port,
                protocol: parsed.protocol,
                status: 'active',
                source: 'manual',
                project_id: projectID,
                tags: []
            };
        });
    };

    const submit = async () => {
        if (busy || !formApi) return;
        setError('');
        try {
            await formApi.validate();
        } catch (validation) {
            return;
        }
        let payload;
        let createdAssets;
        try {
            if (editing) payload = buildPayload();
            else createdAssets = buildCreatedAssets();
        } catch (buildError) {
            setError(buildError.message);
            return;
        }
        setBusy(true);
        try {
            if (editing) {
                await api.put(`/assets/${encodeURIComponent(asset.id)}`, payload);
                toastSuccess(t('assets.updatedSuccessfully', null, '资产已更新'));
            } else {
                const result = await api.post('/assets/import', {
                    assets: createdAssets,
                    source: 'manual'
                });
                const created = Number(result?.created || 0);
                const updated = Number(result?.updated || 0);
                const skipped = Number(result?.skipped || 0);
                const conflicts = Number(result?.project_conflicts || 0);
                const conflictMessage = conflicts ? t('assets.importProjectConflicts', { count: conflicts }, `已跳过 ${conflicts} 条归属不同的重复资产。`) : '';
                if (!created && !updated && skipped) {
                    throw new Error(conflictMessage || t('assets.assetSkipped', null, '资产未保存，可能已存在且你没有更新权限'));
                }
                const parts = [];
                if (created) parts.push(`新增 ${created}`);
                if (updated) parts.push(`合并 ${updated}`);
                if (skipped) parts.push(`跳过 ${skipped}`);
                toastSuccess(parts.length ? `已写入资产库：${parts.join('，')}` : t('assets.createdSuccessfully', null, '资产已添加'));
                if (conflictMessage) toastWarning(conflictMessage);
            }
            onSaved?.();
            onClose?.();
        } catch (submitError) {
            setError(errorMessage(submitError, t('assets.saveFailed', null, '保存资产失败')));
        } finally {
            setBusy(false);
        }
    };

    return (
        <Drawer
            visible={visible}
            title={editing ? t('assets.editAssetTitle', null, '编辑资产') : t('assets.addAssetTitle', null, '新增资产')}
            width={editing ? 760 : 520}
            onCancel={onClose}
            onOk={submit}
            okText={editing ? t('common.save', null, '保存') : t('assets.addAssetAction', null, '添加资产')}
            confirmLoading={busy}
            closeOnEsc
        >
            {error && <Banner type="danger" bordered closeIcon={null} description={error} className="asset-library-form-error" />}
            <Form
                key={`${asset?.id || 'new-asset'}-${visible ? 'open' : 'closed'}`}
                className={FORM_CLASS}
                initValues={values}
                getFormApi={setFormApi}
                labelPosition="top"
                onSubmit={submit}
            >
                {!editing ? (
                    <>
                        <Form.TextArea
                            field="targets"
                            label={t('assets.assetAddress', null, '资产地址')}
                            rows={8}
                            placeholder={'https://example.com/login\nexample.com:443\n1.1.1.1:22'}
                            helpText="每行一个，或用空格、逗号隔开。直接粘贴 URL、域名或 IP:端口。"
                            rules={[{
                                required: true,
                                validator: (rule, value, callback) => {
                                    const targets = splitAssetTargets(value);
                                    if (!targets.length) {
                                        callback(new Error(t('assets.targetRequired', null, '请输入资产地址')));
                                        return;
                                    }
                                    for (const target of targets) {
                                        try {
                                            parseAssetTarget(target);
                                        } catch (parseError) {
                                            callback(new Error(`${target}：${parseError.message}`));
                                            return;
                                        }
                                    }
                                    callback();
                                }
                            }]}
                        />
                        <Form.Select
                            field="project_id"
                            label={t('assets.project', null, '所属项目')}
                            optionList={projectOptions}
                            filter
                            showClear
                            placeholder={t('assets.unboundProject', null, '暂不绑定')}
                            style={{ width: '100%' }}
                        />
                    </>
                ) : (
                    <>
                        <div className="asset-library-form-grid asset-library-form-grid--full">
                            <Form.Input
                                field="target"
                                label={t('assets.assetAddress', null, '资产地址')}
                                placeholder="https://example.com:443 / example.com / 1.1.1.1:22"
                                helpText={t('assets.assetAddressHint', null, '可识别的 URL、域名、IP 和端口会自动解析')}
                                onBlur={() => applyTarget(true)}
                                rules={[
                                    { required: true, message: t('assets.targetRequired', null, '请输入资产地址') },
                                    {
                                        validator: (rule, value, callback) => {
                                            try {
                                                parseAssetTarget(value);
                                                callback();
                                            } catch (parseError) {
                                                callback(new Error(parseError.message));
                                            }
                                        }
                                    }
                                ]}
                            />
                        </div>
                        <div className="asset-library-form-grid">
                            <Form.Input field="host" label={t('assets.hostUrl', null, '完整 URL / Host')} />
                            <Form.Input
                                field="ip"
                                label="IP"
                                rules={[{
                                    validator: (rule, value, callback) => {
                                        if (!value || isIPv4(value) || isIPv6(value)) callback();
                                        else callback(new Error(t('assets.ipInvalid', null, 'IP 地址格式无效')));
                                    }
                                }]}
                            />
                            <Form.Input
                                field="domain"
                                label={t('assets.domain', null, '域名')}
                                rules={[{
                                    validator: (rule, value, callback) => {
                                        if (!value || (normalizeDomain(value) && !isIPv4(value) && !isIPv6(value))) callback();
                                        else callback(new Error(t('assets.domainInvalid', null, '域名格式无效')));
                                    }
                                }]}
                            />
                            <Form.InputNumber
                                field="port"
                                label={t('assets.port', null, '端口')}
                                min={0}
                                max={65535}
                                style={{ width: '100%' }}
                            />
                            <Form.Input
                                field="protocol"
                                label={t('assets.protocol', null, '协议')}
                                rules={[{
                                    validator: (rule, value, callback) => {
                                        if (!value || /^[a-z][a-z0-9+.-]{0,31}$/.test(String(value).toLowerCase())) callback();
                                        else callback(new Error(t('assets.protocolInvalid', null, '协议格式无效')));
                                    }
                                }]}
                            />
                            <Form.Input field="title" label={t('assets.pageTitle', null, '页面标题')} />
                            <Form.Input field="server" label={t('assets.serverProduct', null, '服务 / 产品')} />
                            <Form.Select
                                field="project_id"
                                label={t('assets.project', null, '所属项目')}
                                optionList={projectOptions}
                                filter
                                showClear
                                placeholder={t('assets.unboundProject', null, '暂不绑定')}
                            />
                            <Form.Select field="status" label={t('assets.status', null, '状态')} optionList={STATUS_OPTIONS} />
                            <Form.Select
                                field="environment"
                                label={t('assets.environment', null, '环境')}
                                optionList={ENVIRONMENT_OPTIONS}
                                showClear
                                placeholder="—"
                            />
                            <Form.Select
                                field="criticality"
                                label={t('assets.criticality', null, '重要性')}
                                optionList={CRITICALITY_OPTIONS}
                                showClear
                                placeholder="—"
                            />
                            <Form.Input field="country" label={t('assets.country', null, '国家/地区')} />
                            <Form.Input field="province" label={t('assets.province', null, '省份/州')} />
                            <Form.Input field="city" label={t('assets.city', null, '城市')} />
                            <Form.Input field="responsible_person" label={t('assets.responsiblePerson', null, '负责人')} />
                            <Form.Input field="department" label={t('assets.department', null, '部门')} />
                            <Form.Input field="business_system" label={t('assets.businessSystem', null, '业务系统')} />
                        </div>
                        <Form.TagInput
                            field="tags"
                            label={t('assets.tagsLabel', null, '标签')}
                            placeholder={t('assets.tagsPlaceholder', null, '输入后按回车或逗号')}
                            max={30}
                            maxLength={64}
                            addOnBlur
                            separator=",，"
                            style={{ width: '100%' }}
                        />
                        <Space className="asset-library-editor-actions">
                            <Button theme="borderless" onClick={() => applyTarget(true)}>
                                {t('assets.parseTarget', null, '解析资产地址')}
                            </Button>
                        </Space>
                    </>
                )}
            </Form>
        </Drawer>
    );
}
