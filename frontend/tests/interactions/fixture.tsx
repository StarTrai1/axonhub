import { StrictMode, useState } from 'react';
import { createRoot } from 'react-dom/client';
import '@/index.css';
import { Dialog, DialogContent, DialogTitle, DialogDescription, DialogClose } from '@/components/ui/dialog';
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from '@/components/ui/dropdown-menu';
import { Button } from '@/components/ui/button';
import { AlertDialog, AlertDialogTrigger, AlertDialogContent, AlertDialogTitle, AlertDialogDescription, AlertDialogAction } from '@/components/ui/alert-dialog';

function Fixture() {
  const [dialog, setDialog] = useState(false);
  const [mounted, setMounted] = useState(true);
  const [menu, setMenu] = useState(false);
  const [page, setPage] = useState('management');
  const navigate = () => { setPage('project'); window.history.pushState({}, '', '#project'); };
  return <>
    <Button data-testid="navigation" onClick={navigate}>Project</Button>
    <output data-testid="page">{page}</output>
    <Button data-testid="open-dialog" onClick={() => { setMounted(true); setDialog(true); }}>Open dialog</Button>
    <AlertDialog>
      <AlertDialogTrigger asChild><Button data-testid="open-confirmation">Confirmation</Button></AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogTitle>Offline confirmation</AlertDialogTitle>
        <AlertDialogDescription>No provider request is made.</AlertDialogDescription>
        <AlertDialogAction data-testid="confirm">Confirm</AlertDialogAction>
      </AlertDialogContent>
    </AlertDialog>
    <DropdownMenu open={menu} onOpenChange={setMenu}>
      <DropdownMenuTrigger asChild><Button data-testid="row-menu">Row actions</Button></DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem data-testid="edit" onSelect={() => { setMenu(false); setMounted(true); setTimeout(() => setDialog(true), 0); }}>Edit</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
    {mounted && <Dialog open={dialog} onOpenChange={setDialog}>
      <DialogContent>
        <DialogTitle>Settings</DialogTitle><DialogDescription>Interaction fixture</DialogDescription>
        <DropdownMenu>
          <DropdownMenuTrigger asChild><Button data-testid="nested-menu">Nested actions</Button></DropdownMenuTrigger>
          <DropdownMenuContent>
            <DropdownMenuItem data-testid="close-parent" onSelect={() => { setDialog(false); setMounted(false); }}>Close parent</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <DialogClose asChild><Button data-testid="close-dialog">Close</Button></DialogClose>
      </DialogContent>
    </Dialog>}
  </>;
}

createRoot(document.getElementById('root')!).render(<StrictMode><Fixture /></StrictMode>);
