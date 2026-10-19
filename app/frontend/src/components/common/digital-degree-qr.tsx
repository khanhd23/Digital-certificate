'use client'

import { useRef, useState } from 'react'
import Link from 'next/link'
import { QRCodeSVG } from 'qrcode.react'
import { Download, QrCode } from 'lucide-react'
import { Button } from '../ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from '../ui/dialog'
import VerificationCodeField from './verification-code-field'

// The single public verification link: QR codes, emails and shares all point here.
export const verifyUrl = (id: string) =>
  `${typeof window === 'undefined' ? '' : window.location.origin}/verify/${encodeURIComponent(id)}`

const DigitalDegreeQR: React.FC<{ id: string; name?: string }> = ({ id, name }) => {
  const [open, setOpen] = useState(false)
  const qrRef = useRef<SVGSVGElement>(null)

  const download = () => {
    if (!qrRef.current) return
    const svg = new XMLSerializer().serializeToString(qrRef.current)
    const url = URL.createObjectURL(new Blob([svg], { type: 'image/svg+xml' }))
    const link = document.createElement('a')
    link.href = url
    link.download = `qr-van-bang-${id}.svg`
    link.click()
    URL.revokeObjectURL(url)
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant='outline'>
          <QrCode /> QR xác minh
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Mã QR xác minh văn bằng</DialogTitle>
          <DialogDescription>
            {name ? `${name}. ` : ''}Người nhận quét mã để kiểm tra chữ ký ML-DSA, tệp PDF và Blockchain mà không
            cần đăng nhập.
          </DialogDescription>
        </DialogHeader>
        <div className='mx-auto rounded-2xl border bg-white p-6'>
          {open && <QRCodeSVG ref={qrRef} value={verifyUrl(id)} size={220} level='M' />}
        </div>
        <VerificationCodeField code={id} />
        <div className='flex flex-wrap justify-center gap-2'>
          <Button variant='secondary' onClick={download}>
            <Download /> Tải mã QR
          </Button>
          <Link href={`/verify/${encodeURIComponent(id)}`} target='_blank'>
            <Button variant='outline'>Mở trang xác minh</Button>
          </Link>
        </div>
      </DialogContent>
    </Dialog>
  )
}

export default DigitalDegreeQR
