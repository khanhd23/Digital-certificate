'use client'

import { useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { Button } from '../ui/button'
import { Input } from '../ui/input'

const VerificationCodeField: React.FC<{ code: string }> = ({ code }) => {
  const [copied, setCopied] = useState(false)

  const copyCode = async () => {
    try {
      await navigator.clipboard.writeText(code)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1600)
    } catch {
      setCopied(false)
    }
  }

  return (
    <div className='w-full space-y-2'>
      <p className='text-center text-xs font-medium text-muted-foreground'>Mã xác minh thủ công</p>
      <div className='flex items-center gap-2'>
        <Input value={code} readOnly aria-label='Mã xác minh văn bằng' className='font-mono text-sm' />
        <Button type='button' variant='secondary' onClick={copyCode} aria-label='Sao chép mã xác minh'>
          {copied ? <Check className='text-green-600' /> : <Copy />}
          <span className='hidden sm:inline'>{copied ? 'Đã sao chép' : 'Sao chép'}</span>
        </Button>
      </div>
    </div>
  )
}

export default VerificationCodeField
