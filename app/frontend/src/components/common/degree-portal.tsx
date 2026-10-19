'use client'

import { useState } from 'react'
import useSWR, { mutate as refresh } from 'swr'
import Link from 'next/link'
import { QRCodeSVG } from 'qrcode.react'
import {
  ArrowUpRight,
  FileCheck2,
  ShieldCheck,
  History,
  QrCode,
  RefreshCw,
  Upload,
  CheckCircle2,
  AlertTriangle,
  XCircle,
  CircleDashed,
  Blocks,
  Library,
  Users
} from 'lucide-react'
import apiService from '@/lib/api/root'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogTrigger
} from '@/components/ui/dialog'
import { getUserFriendlyErrorMessage, showNotification } from '@/lib/utils/common'
import { signPQCEDiploma } from '@/lib/api/pqc'
import { ProgressRow, StatTile } from './stat-tile'
import VerificationCodeField from './verification-code-field'

const panel = 'rounded-xl border bg-card p-5 shadow-sm'
const date = (value: string) => new Date(value).toLocaleString('vi-VN')
const fetchPrivate = (url: string) => apiService('GET', url)

export function DegreeSummary() {
  const { data, error, isLoading } = useSWR('degree-summary', fetchPrivate)
  const stats = data?.data
  const n = (key: string) => Number(stats?.[key] ?? 0)
  const show = (key: string) => (isLoading ? '…' : n(key).toLocaleString('vi-VN'))

  if (error) {
    return (
      <p
        role='alert'
        className='rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300'
      >
        Không thể tải thống kê. Vui lòng tải lại trang.
      </p>
    )
  }

  return (
    <div className='space-y-4'>
      <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-4'>
        <StatTile icon={<Users />} label='Sinh viên' value={show('students')} />
        <StatTile icon={<FileCheck2 />} label='Văn bằng đã cấp' value={show('issued')}>
          <p>{(n('total') - n('issued')).toLocaleString('vi-VN')} hồ sơ chờ cấp</p>
        </StatTile>
        <StatTile icon={<ShieldCheck />} label='Đã ký ML-DSA' value={show('signed')} />
        <StatTile icon={<Blocks />} label='Đã ghi Blockchain' value={show('anchored')}>
          {n('revoked') > 0 && (
            <p className='text-red-600 dark:text-red-400'>{n('revoked').toLocaleString('vi-VN')} văn bằng đã thu hồi</p>
          )}
        </StatTile>
      </div>
      <div className='grid gap-4 lg:grid-cols-3'>
        <section className={panel + ' lg:col-span-2'}>
          <h3 className='mb-4 font-semibold'>Tiến độ phát hành</h3>
          <div className='space-y-4'>
            <ProgressRow label='Đã cấp PDF' value={n('issued')} total={n('total')} />
            <ProgressRow label='Đã ký ML-DSA' value={n('signed')} total={n('total')} />
            <ProgressRow label='Đã ghi Blockchain' value={n('anchored')} total={n('total')} />
          </div>
        </section>
        <section className={panel}>
          <h3 className='font-semibold'>Xác minh công khai</h3>
          <p className='mt-4 text-3xl font-semibold tabular-nums'>{show('verification_attempts')}</p>
          <p className='text-sm text-muted-foreground'>lượt xác minh</p>
          <p className='mt-3 text-sm'>
            {n('verification_attempts') > 0
              ? `${Math.round((100 * n('verification_passed')) / n('verification_attempts'))}% hợp lệ đầy đủ`
              : 'Chưa có lượt xác minh'}
          </p>
        </section>
      </div>
    </div>
  )
}

const actionLabel = (action: string) =>
  action.endsWith('/replace')
    ? 'Tạo bản thay thế'
    : action.endsWith('/revoke')
      ? 'Thu hồi'
      : action.includes('/public/') || action.endsWith('/verify')
        ? 'Xác minh công khai'
        : action.includes('/sign')
          ? 'Ký văn bằng'
          : action.includes('/generate')
            ? 'Cấp văn bằng'
            : action.includes('/push-')
              ? 'Ghi Blockchain'
              : action.includes('/users')
                ? 'Quản lý sinh viên'
                : action.includes('/keys')
                  ? 'Quản lý khóa ký'
                  : 'Cập nhật hồ sơ'

export function DegreeHistory({ id }: { id?: string }) {
  const { data, error, isLoading } = useSWR('degree-audit' + (id ? '?diploma_id=' + id : ''), fetchPrivate)
  return (
    <section className={panel + ' my-6'}>
      <h3 className='mb-4 flex items-center gap-2 font-semibold'>
        <History className='size-4 text-main' /> Lịch sử xử lý
      </h3>
      {error ? (
        <p role='alert' className='text-sm text-red-600 dark:text-red-400'>
          Không thể tải nhật ký.
        </p>
      ) : isLoading ? (
        <p className='text-sm text-muted-foreground'>Đang tải…</p>
      ) : !data?.data?.length ? (
        <p className='py-6 text-center text-sm text-muted-foreground'>Chưa có thao tác nào.</p>
      ) : (
        <ol className='max-h-[28rem] divide-y overflow-y-auto pr-2' aria-label='Lịch sử xử lý'>
          {data.data.map((event: any, index: number) => (
            <li key={event._id ?? index} className='flex items-start justify-between gap-3 py-3'>
              <div className='flex items-start gap-3'>
                {event.success === false ? (
                  <AlertTriangle
                    className='mt-0.5 size-4 shrink-0 text-red-600 dark:text-red-400'
                    aria-label='Không thành công'
                  />
                ) : (
                  <CheckCircle2 className='mt-0.5 size-4 shrink-0 text-main' aria-label='Hoàn tất' />
                )}
                <div>
                  <p className='text-sm font-medium'>
                    {event.action?.startsWith('/') ? actionLabel(event.action) : event.action}
                    {event.success === false && (
                      <span className='font-normal text-red-600 dark:text-red-400'> · không thành công</span>
                    )}
                  </p>
                  <p className='text-xs text-muted-foreground'>
                    {event.role === 'public' ? 'Người xác minh công khai' : 'Quản trị trường'}
                  </p>
                </div>
              </div>
              <time className='shrink-0 text-xs text-muted-foreground'>{date(event.at)}</time>
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}

export function DegreeActions({ degree, onChange }: { degree: any; onChange: () => void }) {
  const [qr, setQR] = useState(false)
  const [replace, setReplace] = useState(false)
  const [busy, setBusy] = useState(false)
  const [serial, setSerial] = useState('')
  const [registration, setRegistration] = useState('')
  const [issued, setIssued] = useState('')
  const run = async (action: () => Promise<any>, message: string) => {
    setBusy(true)
    try {
      await action()
      showNotification('success', message)
      setReplace(false)
      onChange()
      refresh('degree-summary')
      refresh('degree-audit')
    } catch (e: any) {
      showNotification('error', getUserFriendlyErrorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <>
      {degree.issued && (
        <Dialog open={qr} onOpenChange={setQR}>
          <DialogTrigger asChild>
            <Button size='icon' variant='outline' title='QR xác minh công khai' aria-label='QR xác minh công khai'>
              <QrCode size={18} />
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Chia sẻ văn bằng</DialogTitle>
              <DialogDescription>
                Quét mã để xác minh chữ ký, PDF và trạng thái hiện tại. Không cần đăng nhập.
              </DialogDescription>
            </DialogHeader>
            <div className='mx-auto rounded-2xl border bg-white p-6'>
              {qr && <QRCodeSVG value={`${window.location.origin}/verify/${degree.id}`} size={220} level='M' />}
            </div>
            <VerificationCodeField code={degree.id} />
            <Link
              className='text-center text-sm text-indigo-600 underline'
              href={`/verify/${degree.id}`}
              target='_blank'
            >
              Mở trang xác minh
            </Link>
            <p className='text-center text-xs text-slate-500'>
              Người có liên kết có thể xem thông tin văn bằng công khai.
            </p>
          </DialogContent>
        </Dialog>
      )}
      {degree.issued && !degree.revoked && !degree.signed && !degree.pqc_proof && (
        <Button
          disabled={busy}
          variant='outline'
          title='Ký ML-DSA'
          onClick={() => run(() => signPQCEDiploma(degree.id), 'Đã ký văn bằng bằng ML-DSA')}
        >
          <ShieldCheck size={16} /> Ký
        </Button>
      )}
      {degree.revoked && (
        <Dialog open={replace} onOpenChange={setReplace}>
          <DialogTrigger asChild>
            <Button variant='outline' className='text-indigo-600'>
              <RefreshCw size={16} /> Cấp thay thế
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Tạo văn bằng thay thế</DialogTitle>
              <DialogDescription>
                Giữ lại bản đã thu hồi. Bản mới kế thừa thông tin học tập và cần được cấp, ký lại trong danh sách “Chưa
                cấp”.
              </DialogDescription>
            </DialogHeader>
            <label className='space-y-2 text-sm'>
              Số hiệu mới
              <Input value={serial} onChange={(e) => setSerial(e.target.value)} />
            </label>
            <label className='space-y-2 text-sm'>
              Số vào sổ mới
              <Input value={registration} onChange={(e) => setRegistration(e.target.value)} />
            </label>
            <label className='space-y-2 text-sm'>
              Ngày cấp mới
              <Input type='date' value={issued} onChange={(e) => setIssued(e.target.value)} />
            </label>
            <Button
              disabled={busy || !serial.trim() || !registration.trim() || !issued}
              onClick={() =>
                run(
                  () =>
                    apiService('POST', `ediplomas/${degree.id}/replace`, {
                      serial_number: serial,
                      registration_number: registration,
                      issue_date: new Date(issued).toISOString()
                    }),
                  'Đã tạo bản thay thế trong danh sách chưa cấp'
                )
              }
            >
              {busy ? 'Đang tạo…' : 'Tạo bản thay thế'}
            </Button>
          </DialogContent>
        </Dialog>
      )}
    </>
  )
}

export function PublicDegree({ id }: { id: string }) {
  const { data, error, isLoading, mutate } = useSWR(
    `public/degrees/${id}`,
    (url) => apiService('GET', url, undefined, false),
    { shouldRetryOnError: false, revalidateOnFocus: false }
  )
  const [fileResult, setFileResult] = useState<{ name: string; hash: string; match: boolean } | null>(null)
  const [checking, setChecking] = useState(false)
  const [fileError, setFileError] = useState('')
  const d = data?.data,
    v = d?.verification
  const checkFile = async (file?: File) => {
    setFileResult(null)
    setFileError('')
    if (!file) return
    if (file.size > 25 * 1024 * 1024) {
      setFileError('Vui lòng chọn PDF dưới 25 MB.')
      return
    }
    setChecking(true)
    try {
      const digest = await crypto.subtle.digest('SHA-256', await file.arrayBuffer())
      const hash = Array.from(new Uint8Array(digest))
        .map((x) => x.toString(16).padStart(2, '0'))
        .join('')
      setFileResult({ name: file.name, hash, match: hash === d.file_hash })
    } catch {
      setFileError('Không thể đọc tệp. Hãy dùng HTTPS hoặc localhost.')
    } finally {
      setChecking(false)
    }
  }
  const complete = !d?.revoked && v?.valid
  const signed = !d?.revoked && v?.signature_valid && v?.file_integrity_valid
  const title = d?.revoked
    ? 'Văn bằng đã thu hồi'
    : complete
      ? 'Xác minh thành công'
      : signed
        ? 'Chữ ký và PDF hợp lệ'
        : 'Chưa xác minh được văn bằng'
  return (
    <main className='min-h-screen bg-slate-50 px-4 py-10 text-slate-900 dark:bg-slate-900 dark:text-slate-100'>
      <div className='mx-auto max-w-4xl'>
        <div className='mb-8 flex items-center justify-between'>
          <Link href='/' className='flex items-center gap-2 font-semibold'>
            <ShieldCheck className='text-indigo-600' /> Văn bằng số PQC
          </Link>
          <span className='text-xs text-slate-500'>CỔNG XÁC MINH CÔNG KHAI</span>
        </div>
        {isLoading ? (
          <div className={panel + ' animate-pulse'}>Đang kiểm tra chữ ký, tệp PDF và Blockchain…</div>
        ) : error ? (
          <div className={panel}>
            <AlertTriangle className='mb-3 text-amber-600' />
            <h1 className='text-xl font-semibold'>Không thể xác minh</h1>
            <p className='my-3 text-sm'>{getUserFriendlyErrorMessage(error)}</p>
            <Button onClick={() => mutate()}>Thử lại</Button>
          </div>
        ) : (
          d && (
            <>
              <section
                className={
                  'rounded-2xl border p-7 ' +
                  (d.revoked
                    ? 'border-rose-300 bg-rose-50 text-rose-900 dark:border-rose-800 dark:bg-rose-950/50 dark:text-rose-100'
                    : complete
                      ? 'border-emerald-300 bg-emerald-100 text-emerald-900 dark:border-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-50'
                      : signed
                        ? 'border-emerald-200 bg-emerald-50 text-emerald-900 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-100'
                        : 'border-amber-300 bg-amber-50 text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-100')
                }
              >
                <div className='flex items-start gap-4'>
                  {d.revoked ? (
                    <XCircle size={32} className='shrink-0 text-rose-600 dark:text-rose-400' />
                  ) : complete || signed ? (
                    <CheckCircle2 size={32} className='shrink-0 text-emerald-600 dark:text-emerald-400' />
                  ) : (
                    <AlertTriangle size={32} className='shrink-0 text-amber-600 dark:text-amber-400' />
                  )}
                  <div>
                    <p className='mb-2 text-xs font-semibold uppercase tracking-widest'>Kết quả xác minh</p>
                    <h1 className='text-2xl font-semibold'>{title}</h1>
                    <p className='mt-2 text-sm'>
                      {d.revoked
                        ? d.revocation_on_chain
                          ? 'Văn bằng này không còn hiệu lực. Việc thu hồi đã được ghi trên Blockchain.'
                          : 'Văn bằng này không còn hiệu lực, kể cả khi chữ ký gốc vẫn đúng.'
                        : complete
                          ? `Chữ ký ${v?.algorithm || 'ML-DSA'}, tệp PDF và bằng chứng Blockchain đều khớp.`
                          : signed
                            ? 'Bằng chứng Blockchain chưa được xác nhận. Xem riêng từng kết quả bên dưới.'
                            : 'Kiểm tra các kết quả bên dưới hoặc liên hệ đơn vị cấp.'}
                    </p>
                  </div>
                </div>
              </section>
              <section className={panel + ' my-5'}>
                <p className='text-xs font-semibold uppercase tracking-widest text-indigo-600'>
                  {d.certificate_type || 'Văn bằng'}
                </p>
                <h2 className='mt-2 text-3xl font-semibold'>{d.full_name}</h2>
                <p className='mt-2 text-slate-500'>
                  {d.name} · {d.university_name}
                </p>
                <dl className='mt-6 grid gap-5 border-t pt-5 sm:grid-cols-2'>
                  <div>
                    <dt className='text-xs text-slate-500'>Số hiệu</dt>
                    <dd className='mt-1 font-medium'>{d.serial_number}</dd>
                  </div>
                  <div>
                    <dt className='text-xs text-slate-500'>Ngày cấp</dt>
                    <dd className='mt-1 font-medium'>{new Date(d.issue_date).toLocaleDateString('vi-VN')}</dd>
                  </div>
                </dl>
              </section>
              <div className='grid gap-3 sm:grid-cols-3'>
                {(
                  [
                    [
                      `Chữ ký ${v?.algorithm || 'ML-DSA'}`,
                      v?.signature_valid ? 'pass' : 'fail',
                      v?.signature_valid ? 'Hợp lệ' : 'Chưa đạt'
                    ],
                    [
                      'Toàn vẹn PDF',
                      v?.file_integrity_checked ? (v.file_integrity_valid ? 'pass' : 'fail') : 'pending',
                      v?.file_integrity_checked
                        ? v.file_integrity_valid
                          ? 'Khớp bản đã ký'
                          : 'Không khớp'
                        : 'Chưa kiểm tra được'
                    ],
                    [
                      'Blockchain',
                      v?.blockchain?.valid ? 'pass' : v?.blockchain?.status === 'not_anchored' ? 'pending' : 'fail',
                      v?.blockchain?.valid
                        ? 'Đã xác nhận'
                        : v?.blockchain?.status === 'not_anchored'
                          ? 'Chưa ghi Blockchain'
                          : 'Chưa xác nhận'
                    ]
                  ] as const
                ).map(([label, state, value]) => (
                  <div
                    key={label}
                    className={
                      'rounded-xl border p-4 shadow-sm ' +
                      (state === 'pass'
                        ? 'border-emerald-200 bg-emerald-50 dark:border-emerald-800 dark:bg-emerald-950/40'
                        : state === 'fail'
                          ? 'border-rose-200 bg-rose-50 dark:border-rose-800 dark:bg-rose-950/40'
                          : 'bg-card')
                    }
                  >
                    <p className='text-xs text-slate-500 dark:text-slate-400'>{label}</p>
                    <p
                      className={
                        'mt-2 flex items-center gap-1.5 font-semibold ' +
                        (state === 'pass'
                          ? 'text-emerald-700 dark:text-emerald-300'
                          : state === 'fail'
                            ? 'text-rose-700 dark:text-rose-300'
                            : 'text-slate-600 dark:text-slate-300')
                      }
                    >
                      {state === 'pass' ? (
                        <CheckCircle2 size={16} />
                      ) : state === 'fail' ? (
                        <XCircle size={16} />
                      ) : (
                        <CircleDashed size={16} />
                      )}
                      {value}
                    </p>
                  </div>
                ))}
              </div>
              <section className={panel + ' my-5'}>
                <h3 className='flex items-center gap-2 font-semibold'>
                  <Upload size={18} /> Đối chiếu tệp PDF của bạn
                </h3>
                <p className='mb-5 mt-2 text-sm text-slate-500'>
                  Chọn bản gốc hoặc bản đã chỉnh sửa để kiểm tra. Tệp được băm ngay trên trình duyệt, không tải lên máy
                  chủ.
                </p>
                <Input
                  aria-label='Chọn tệp PDF cần đối chiếu'
                  type='file'
                  accept='.pdf,application/pdf'
                  disabled={checking}
                  onChange={(e) => checkFile(e.target.files?.[0])}
                />
                {checking && <p className='mt-3'>Đang tính mã băm…</p>}
                {fileError && (
                  <p role='alert' className='mt-3 text-red-600'>
                    {fileError}
                  </p>
                )}
                {fileResult && (
                  <div
                    role='status'
                    className={
                      'mt-4 rounded-xl p-4 ' +
                      (fileResult.match ? 'bg-emerald-50 text-emerald-800' : 'bg-rose-50 text-rose-800')
                    }
                  >
                    <p className='font-medium'>
                      {fileResult.match ? 'Tệp trùng khớp với bản được lưu' : 'Tệp không khớp — có thể đã bị thay đổi'}
                    </p>
                    <p className='mt-2 break-all text-xs'>
                      {fileResult.name} · SHA-256: {fileResult.hash}
                    </p>
                    <p className='mt-2 text-xs'>
                      Kết quả đối chiếu tệp không thay thế việc kiểm tra chữ ký và trạng thái thu hồi.
                    </p>
                  </div>
                )}
              </section>
              <p className='text-center text-xs text-slate-500'>
                Kết quả được kiểm tra tại thời điểm truy cập. Đơn vị cấp chịu trách nhiệm về nội dung văn bằng.
              </p>
            </>
          )
        )}
      </div>
    </main>
  )
}

const QUICK_LINKS = [
  { href: '/education-admin/faculty-management', icon: <Library />, title: 'Chuyên ngành' },
  { href: '/education-admin/student-management', icon: <Users />, title: 'Hồ sơ sinh viên' },
  { href: '/education-admin/digital-degree-management', icon: <ShieldCheck />, title: 'Văn bằng số & khóa ký' }
]

export function DegreeDashboard() {
  return (
    <div className='space-y-6'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <h2>Tổng quan</h2>
        <Link href='/education-admin/digital-degree-management'>
          <Button>
            Quản lý văn bằng <ArrowUpRight />
          </Button>
        </Link>
      </div>
      <DegreeSummary />
      <div className='grid gap-4 sm:grid-cols-3'>
        {QUICK_LINKS.map((item) => (
          <Link
            key={item.href}
            href={item.href}
            className={panel + ' group flex items-center gap-3 transition-colors hover:border-main'}
          >
            <span className='rounded-lg bg-blue-50 p-2 text-main dark:bg-blue-950/60 [&_svg]:size-4'>{item.icon}</span>
            <span className='font-medium'>{item.title}</span>
            <ArrowUpRight className='ml-auto size-4 text-muted-foreground transition-colors group-hover:text-main' />
          </Link>
        ))}
      </div>
      <DegreeHistory />
    </div>
  )
}
