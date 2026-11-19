/** 批量导入：CSV / JSON 文本或文件上传，先校验预览再提交 /assets/import。 */
import { useMemo, useRef, useState } from 'react';
import {
    Banner, Button, RadioGroup, Space, Table, TextArea, Typography
} from '@douyinfe/semi-ui';
import { IconCloudUploadStroked } from '@douyinfe/semi-icons';
import { api, errorMessage } from '../../../lib/http';
import { useT } from '../../../lib/i18n';
import { Drawer, toastSuccess } from '../../../components/ui';
import { assetTargetLabel } from './assetTarget';
import { csvTemplate, downloadTextFile, recordsFromCsv, recordsFromJson } from './assetCsv';

const { Text } = Typography;

const PREVIEW_COLUMNS = (t) => [
    { title: t('assets.rowNumber', null, '行号'), dataIndex: 'rowNumber', width: 70 },
    {
        title: t('assets.target', null, '目标'),
        dataIndex: 'target',
        render: (_, record) => assetTargetLabel(record.asset || { target: record.values?.target })
    },
    {
        title: t('assets.project', null, '所属项目'),
        dataIndex: 'project',
        render: (_, record) => record.projectName || record.values?.project || '—'
    },
    {
        title: t('assets.status', null, '状态'),
        dataIndex: 'status',
        render: (_, record) => record.asset?.status || record.values?.status || '—'
    },
    {
        title: t('assets.validationResult', null, '校验结果'),
        dataIndex: 'error',
        render: value => (value
            ? <Text type="danger" size="small">{value}</Text>
            : <Text type="success" size="small">{t('assets.validationPassed', null, '通过')}</Text>)
    }
];

export default function AssetImportModal({ visible, projects, onClose, onDone }) {
    const t = useT();
    const [format, setFormat] = useState('csv');
    const [text, setText] = useState('');
    const [fileName, setFileName] = useState('');
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);
    const fileInputRef = useRef(null);

    const parsed = useMemo(() => {
        if (!String(text || '').trim()) return { records: [], error: '' };
        try {
            const records = format === 'csv'
                ? recordsFromCsv(text, { projects, source: 'manual-import' })
                : recordsFromJson(text, { projects, source: 'manual-import' });
            return { records, error: '' };
        } catch (parseError) {
            return { records: [], error: parseError.message };
        }
    }, [text, format, projects]);

    const records = parsed.records;
    const valid = records.filter(record => !record.error);
    const invalid = records.length - valid.length;
    const displayError = error || parsed.error;

    const reset = () => {
        setText('');
        setFileName('');
        setFormat('csv');
        setError('');
        setBusy(false);
    };

    const close = () => {
        if (busy) return;
        reset();
        onClose?.();
    };

    const handleFile = async file => {
        if (!file) return;
        setError('');
        const name = String(file.name || '');
        const extension = name.split('.').pop().toLowerCase();
        if (!['csv', 'json', 'txt'].includes(extension)) {
            setError('仅支持 .csv / .json / .txt 文件');
            return;
        }
        if (file.size > 100 * 1024 * 1024) {
            setError(t('assets.fileTooLarge', null, '文件不能超过 100 MB'));
            return;
        }
        try {
            const content = await file.text();
            setFormat(extension === 'json' ? 'json' : 'csv');
            setText(content);
            setFileName(name);
        } catch (readError) {
            setError(errorMessage(readError, t('assets.fileParseFailed', null, '无法解析文件，请确认文件格式正确')));
        }
    };

    const submit = async () => {
        if (busy || !valid.length) return;
        setError('');
        setBusy(true);
        try {
            const result = await api.post('/assets/import', {
                assets: valid.map(record => record.asset),
                source: 'manual-import',
                source_query: fileName || 'paste'
            });
            const skipped = Number(result?.skipped || 0) + invalid;
            toastSuccess(t('assets.bulkImportDone', {
                created: Number(result?.created || 0),
                updated: Number(result?.updated || 0),
                skipped
            }, `导入完成：新增 ${Number(result?.created || 0)} 条，更新 ${Number(result?.updated || 0)} 条，跳过 ${skipped} 条`));
            reset();
            onDone?.();
            onClose?.();
        } catch (submitError) {
            setError(errorMessage(submitError, t('assets.importFailed', null, '资产入库失败')));
        } finally {
            setBusy(false);
        }
    };

    return (
        <Drawer
            visible={visible}
            title={t('assets.bulkImportTitle', null, '批量导入资产')}
            width={880}
            onCancel={close}
            onOk={submit}
            okText={t('assets.importValidRowsCount', { count: valid.length }, `导入 ${valid.length} 条有效数据`)}
            okButtonProps={{ disabled: !valid.length }}
            confirmLoading={busy}
            maskClosable={false}
        >
            <Text type="tertiary">
                {t('assets.bulkImportSubtitle', null, '支持 CSV / JSON 文本粘贴或文件上传，提交前会先校验并预览数据')}
            </Text>
            {displayError && (
                <Banner type="danger" bordered closeIcon={null} description={displayError} className="asset-library-form-error" />
            )}
            <Space vertical align="start" spacing={10} className="asset-library-import-body">
                <Space>
                    <RadioGroup
                        type="button"
                        value={format}
                        options={[
                            { value: 'csv', label: 'CSV' },
                            { value: 'json', label: 'JSON' }
                        ]}
                        onChange={event => { setFormat(event.target.value); setError(''); }}
                    />
                    <Button
                        icon={<IconCloudUploadStroked />}
                        onClick={() => fileInputRef.current?.click()}
                    >
                        {t('assets.chooseOrDropFile', null, '选择文件')}
                    </Button>
                    <input
                        ref={fileInputRef}
                        type="file"
                        accept=".csv,.json,.txt"
                        className="asset-library-file-input"
                        onChange={event => {
                            handleFile(event.target.files?.[0]);
                            event.target.value = '';
                        }}
                    />
                    <Button
                        theme="borderless"
                        onClick={() => downloadTextFile('asset-import-template.csv', csvTemplate())}
                    >
                        {t('assets.downloadCsv', null, '下载 CSV 模板')}
                    </Button>
                    {fileName && <Text type="tertiary" size="small">{fileName}</Text>}
                </Space>
                <TextArea
                    value={text}
                    rows={8}
                    className="asset-library-import-textarea"
                    placeholder={format === 'csv'
                        ? 'target,project,tags,host,ip,domain,port,protocol,title,server,country,province,city,responsible_person,department,business_system,environment,criticality,status'
                        : '[{"host":"https://example.com","ip":"1.1.1.1","port":443,"protocol":"https","tags":["prod"]}]'}
                    onChange={value => { setText(value); setError(''); }}
                />
                {records.length > 0 && (
                    <div className="asset-library-import-preview">
                        <Text strong>
                            {t('assets.importPreviewSummary', {
                                total: records.length, valid: valid.length, invalid
                            }, `共 ${records.length} 行，${valid.length} 行有效，${invalid} 行需修正`)}
                        </Text>
                        <Table
                            columns={PREVIEW_COLUMNS(t)}
                            dataSource={records.slice(0, 10).map((record, index) => ({ ...record, id: index }))}
                            rowKey="id"
                            size="small"
                            pagination={false}
                        />
                        {records.length > 10 && (
                            <Text type="tertiary" size="small">
                                {t('assets.previewLimitedShort', { count: records.length }, `仅展示前 10 行；提交时将处理全部 ${records.length} 行`)}
                            </Text>
                        )}
                    </div>
                )}
            </Space>
        </Drawer>
    );
}
