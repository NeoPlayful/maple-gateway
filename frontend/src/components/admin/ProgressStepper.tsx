// 签发/续期进度条：按阶段顺序展示当前进行到哪一步。
// 阶段顺序与后端 certificate.OperationSteps 保持一致。
import { useTranslation } from 'react-i18next';
import { CheckIcon, XMarkIcon } from '@heroicons/react/24/outline';

export const OPERATION_STEPS = [
  'queued',
  'account_ready',
  'order_created',
  'challenge_presented',
  'challenge_validated',
  'finalizing',
  'active',
] as const;

export function ProgressStepper({
  status,
  message,
  error,
}: {
  status: string;
  message?: string;
  error?: string;
}) {
  const { t } = useTranslation('admin');
  const failed = status === 'failed';
  const currentIdx = OPERATION_STEPS.indexOf(status as (typeof OPERATION_STEPS)[number]);

  return (
    <div>
      <ol className="space-y-2">
        {OPERATION_STEPS.map((step, i) => {
          const done = !failed && (status === 'active' || i < currentIdx);
          const current = !failed && i === currentIdx && status !== 'active';
          return (
            <li key={step} className="flex items-center gap-3 text-sm">
              <span
                className={`flex h-5 w-5 shrink-0 items-center justify-center rounded-full ${
                  done
                    ? 'bg-emerald-500 text-white'
                    : current
                      ? 'bg-th-accent text-white'
                      : 'bg-slate-200 text-slate-400 dark:bg-slate-700 dark:text-slate-500'
                }`}
              >
                {done ? (
                  <CheckIcon className="h-3.5 w-3.5" />
                ) : current ? (
                  <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-white" />
                ) : (
                  <span className="h-1.5 w-1.5 rounded-full bg-current" />
                )}
              </span>
              <span
                className={
                  done || current
                    ? 'text-slate-800 dark:text-slate-200'
                    : 'text-slate-400 dark:text-slate-500'
                }
              >
                {t(`operation.step.${step}`)}
              </span>
            </li>
          );
        })}
      </ol>
      {failed && (
        <div className="mt-3 rounded border border-rose-200 bg-rose-50 p-2.5 text-xs dark:border-rose-900/60 dark:bg-rose-950/40">
          <div className="flex items-start gap-2 text-rose-700 dark:text-rose-300">
            <XMarkIcon className="mt-0.5 h-4 w-4 shrink-0" />
            <div>
              {message && <div>{message}</div>}
              {error && <div className="mt-0.5 font-medium">{error}</div>}
            </div>
          </div>
        </div>
      )}
      {!failed && message && (
        <div className="mt-3 text-xs text-slate-500 dark:text-slate-400">{message}</div>
      )}
    </div>
  );
}
