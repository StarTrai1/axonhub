import { StrictMode, useState } from 'react';
import { createRoot } from 'react-dom/client';
import '@/index.css';
import { ModelIcon } from '@/features/models/components/model-icon';

function Fixture() {
  const [name, setName] = useState('OpenAI');
  return <>
    {['OpenAI', 'Anthropic', 'Qwen', 'AionLabs', 'Dolphin', 'missing-icon'].map((name) => <div key={name} data-testid={name}><ModelIcon name={name} /></div>)}
    <div data-testid='changing-icon'><ModelIcon name={name} /></div>
    <button onClick={() => setName('Google')}>Change</button>
  </>;
}

createRoot(document.getElementById('root')!).render(<StrictMode><Fixture /></StrictMode>);
